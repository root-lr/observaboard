package compose

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Stage is one build stage of a Dockerfile.
type Stage struct {
	Name       string   // AS name, lower-cased ("" if none)
	Base       string   // FROM image or earlier stage name
	Cmd        []string // exec form, or ["/bin/sh","-c",...] for shell form
	Entrypoint []string
	User       string // last USER of this stage ("" if none)
	EndLine    int    // index (0-based) of the last raw line belonging to the stage
}

// Dockerfile holds what ObservaBoard reads from a Dockerfile.
type Dockerfile struct {
	Stages []Stage
	Raw    string
	lines  []string
}

// ParseDockerfile reads a Dockerfile and splits it into stages.
func ParseDockerfile(path string) (*Dockerfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDockerfileString(string(data)), nil
}

// ParseDockerfileString parses Dockerfile content.
func ParseDockerfileString(src string) *Dockerfile {
	d := &Dockerfile{Raw: src, lines: strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")}
	var (
		buf   strings.Builder
		start = -1
	)
	cur := -1
	handle := func(line string, last int) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		kw, args, _ := strings.Cut(line, " ")
		kw = strings.ToUpper(kw)
		args = strings.TrimSpace(args)
		if kw == "FROM" {
			st := Stage{}
			var parts []string
			for _, f := range strings.Fields(args) {
				if !strings.HasPrefix(f, "--") {
					parts = append(parts, f)
				}
			}
			if len(parts) > 0 {
				st.Base = parts[0]
			}
			if len(parts) >= 3 && strings.EqualFold(parts[1], "AS") {
				st.Name = strings.ToLower(parts[2])
			}
			d.Stages = append(d.Stages, st)
			cur = len(d.Stages) - 1
		}
		if cur < 0 {
			return // ARG before the first FROM
		}
		st := &d.Stages[cur]
		st.EndLine = last
		switch kw {
		case "CMD":
			st.Cmd = parseCmd(args)
		case "ENTRYPOINT":
			st.Entrypoint = parseCmd(args)
		case "USER":
			st.User = args
		}
	}
	for i, raw := range d.lines {
		t := strings.TrimSpace(raw)
		if buf.Len() == 0 && (t == "" || strings.HasPrefix(t, "#")) {
			if cur >= 0 {
				d.Stages[cur].EndLine = i
			}
			continue
		}
		if start < 0 {
			start = i
		}
		if strings.HasPrefix(t, "#") { // comment inside a continuation
			continue
		}
		if strings.HasSuffix(t, "\\") {
			buf.WriteString(strings.TrimSuffix(t, "\\"))
			buf.WriteString(" ")
			continue
		}
		buf.WriteString(t)
		handle(buf.String(), i)
		buf.Reset()
		start = -1
	}
	if buf.Len() > 0 {
		handle(buf.String(), len(d.lines)-1)
	}
	return d
}

// StageIndex returns the stage a build targets: the named one, or the last.
func (d *Dockerfile) StageIndex(target string) (int, error) {
	if len(d.Stages) == 0 {
		return -1, fmt.Errorf("aucune instruction FROM")
	}
	if target == "" {
		return len(d.Stages) - 1, nil
	}
	for i, s := range d.Stages {
		if s.Name == strings.ToLower(target) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("étape %q introuvable dans le Dockerfile", target)
}

// resolve follows FROM <earlier stage> to find inherited values.
func (d *Dockerfile) resolve(i int, get func(Stage) bool) (Stage, bool) {
	seen := map[int]bool{}
	for i >= 0 && !seen[i] {
		seen[i] = true
		if get(d.Stages[i]) {
			return d.Stages[i], true
		}
		parent := -1
		for j := 0; j < i; j++ {
			if d.Stages[j].Name != "" && d.Stages[j].Name == strings.ToLower(d.Stages[i].Base) {
				parent = j
			}
		}
		i = parent
	}
	return Stage{}, false
}

// User returns the effective USER of a stage, following stage inheritance.
// It returns "" when no USER is set in the chain (the base image decides).
func (d *Dockerfile) User(i int) string {
	s, _ := d.resolve(i, func(s Stage) bool { return s.User != "" })
	return s.User
}

// Cmd returns the effective CMD of a stage, following stage inheritance.
func (d *Dockerfile) Cmd(i int) []string {
	s, _ := d.resolve(i, func(s Stage) bool { return s.Cmd != nil })
	return s.Cmd
}

// Entrypoint returns the effective ENTRYPOINT of a stage.
func (d *Dockerfile) Entrypoint(i int) []string {
	s, _ := d.resolve(i, func(s Stage) bool { return s.Entrypoint != nil })
	return s.Entrypoint
}

// ExternalBase returns the external image a stage chain starts from.
func (d *Dockerfile) ExternalBase(i int) string {
	s, ok := d.resolve(i, func(s Stage) bool {
		for _, o := range d.Stages {
			if o.Name != "" && o.Name == strings.ToLower(s.Base) {
				return false
			}
		}
		return true
	})
	if !ok {
		return ""
	}
	return s.Base
}

// InsertAfterStage returns the Dockerfile with block inserted at the end of
// stage i (before the next FROM when there is one).
func (d *Dockerfile) InsertAfterStage(i int, block string) string {
	end := d.Stages[i].EndLine
	var b strings.Builder
	for j, l := range d.lines {
		if j > 0 {
			b.WriteString("\n")
		}
		b.WriteString(l)
		if j == end {
			b.WriteString("\n" + strings.TrimRight(block, "\n"))
			if i < len(d.Stages)-1 {
				b.WriteString("\n")
			}
		}
	}
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func parseCmd(args string) []string {
	args = strings.TrimSpace(args)
	if strings.HasPrefix(args, "[") {
		var l []string
		if err := json.Unmarshal([]byte(args), &l); err == nil {
			return l
		}
	}
	if args == "" {
		return nil
	}
	return []string{"/bin/sh", "-c", args}
}
