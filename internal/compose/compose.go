// Package compose reads the parts of a Docker Compose file that ObservaBoard
// needs (services, images, builds, commands, networks) and parses Dockerfiles
// just enough to recover the default command and user.
package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

// CandidateNames are the file names `docker compose` looks for, in order.
var CandidateNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// Find returns the compose file in dir, or "" when there is none.
func Find(dir string) string {
	for _, n := range CandidateNames {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// File is a parsed compose file.
type File struct {
	Path     string
	Dir      string
	Services map[string]*Service
}

// Build is the build section of a service.
type Build struct {
	Context    string // relative to the compose file directory
	Dockerfile string // relative to Context
	Target     string // build stage, "" = last
}

// Service holds the fields ObservaBoard reads.
type Service struct {
	Name          string
	Image         string
	Build         *Build
	Command       []string // nil when not set
	Entrypoint    []string // nil when not set
	Networks      []string // explicit networks, nil when not declared
	NetworkMode   string   // network_mode (host, none, service:x…): no networks allowed
	ContainerName string
	Ports         []string
	Expose        []string
	Environment   map[string]string
	Labels        map[string]string
}

// Load parses a compose file.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	f := &File{Path: path, Dir: filepath.Dir(path), Services: map[string]*Service{}}
	svcs, _ := raw["services"].(map[string]any)
	if len(svcs) == 0 {
		return nil, fmt.Errorf("%s contains no services", filepath.Base(path))
	}
	for name, v := range svcs {
		m, _ := v.(map[string]any)
		if m == nil {
			m = map[string]any{}
		}
		s := &Service{Name: name}
		s.Image, _ = m["image"].(string)
		s.NetworkMode, _ = m["network_mode"].(string)
		s.ContainerName, _ = m["container_name"].(string)
		switch b := m["build"].(type) {
		case string:
			s.Build = &Build{Context: b, Dockerfile: "Dockerfile"}
		case map[string]any:
			ctx, _ := b["context"].(string)
			if ctx == "" {
				ctx = "."
			}
			df, _ := b["dockerfile"].(string)
			if df == "" {
				df = "Dockerfile"
			}
			target, _ := b["target"].(string)
			s.Build = &Build{Context: ctx, Dockerfile: df, Target: target}
		}
		var err error
		if s.Command, err = commandValue(m["command"]); err != nil {
			return nil, fmt.Errorf("services.%s.command: %w", name, err)
		}
		if s.Entrypoint, err = commandValue(m["entrypoint"]); err != nil {
			return nil, fmt.Errorf("services.%s.entrypoint: %w", name, err)
		}
		switch n := m["networks"].(type) {
		case []any:
			s.Networks = []string{}
			for _, x := range n {
				if str, ok := x.(string); ok {
					s.Networks = append(s.Networks, str)
				}
			}
		case map[string]any:
			s.Networks = []string{}
			for k := range n {
				s.Networks = append(s.Networks, k)
			}
			sort.Strings(s.Networks)
		}
		s.Ports = stringList(m["ports"])
		s.Expose = stringList(m["expose"])
		s.Environment = keyValues(m["environment"])
		s.Labels = keyValues(m["labels"])
		f.Services[name] = s
	}
	return f, nil
}

// ServiceNames returns the service names, sorted.
func (f *File) ServiceNames() []string {
	var n []string
	for k := range f.Services {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// ContextDir returns the absolute build context directory of a service.
func (f *File) ContextDir(s *Service) string {
	if s.Build == nil {
		return f.Dir
	}
	return filepath.Join(f.Dir, s.Build.Context)
}

// DockerfilePath returns the absolute Dockerfile path of a service, or "".
func (f *File) DockerfilePath(s *Service) string {
	if s.Build == nil {
		return ""
	}
	if filepath.IsAbs(s.Build.Dockerfile) {
		return s.Build.Dockerfile
	}
	return filepath.Join(f.ContextDir(s), s.Build.Dockerfile)
}

// ContainerPorts returns the container-side ports declared by a service.
func (s *Service) ContainerPorts() []int {
	var out []int
	for _, p := range append(append([]string{}, s.Ports...), s.Expose...) {
		p = strings.SplitN(p, "/", 2)[0] // drop /tcp
		parts := strings.Split(p, ":")
		last := parts[len(parts)-1]
		last = strings.SplitN(last, "-", 2)[0] // ranges: keep the first
		if n, err := strconv.Atoi(strings.TrimSpace(last)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func commandValue(v any) ([]string, error) {
	switch c := v.(type) {
	case nil:
		return nil, nil
	case string:
		return SplitWords(c)
	case []any:
		out := []string{}
		for _, x := range c {
			out = append(out, fmt.Sprint(x))
		}
		return out, nil
	default:
		return nil, errors.New("unrecognized format")
	}
}

func stringList(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			switch t := x.(type) {
			case string:
				out = append(out, t)
			case int:
				out = append(out, strconv.Itoa(t))
			case map[string]any: // long port syntax
				if tp, ok := t["target"]; ok {
					out = append(out, fmt.Sprint(tp))
				}
			}
		}
	}
	return out
}

func keyValues(v any) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if x == nil {
				out[k] = ""
			} else {
				out[k] = fmt.Sprint(x)
			}
		}
	case []any:
		for _, x := range t {
			s := fmt.Sprint(x)
			k, val, _ := strings.Cut(s, "=")
			out[k] = val
		}
	}
	return out
}

// SplitWords splits a command line like a POSIX shell would split words
// (quotes and backslashes), which is what Compose does for string commands.
func SplitWords(s string) ([]string, error) {
	var (
		out   []string
		cur   strings.Builder
		inTok bool
		quote rune
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			if r == '"' {
				quote = 0
			} else if r == '\\' && i+1 < len(rs) && strings.ContainsRune(`"\$`+"`", rs[i+1]) {
				i++
				cur.WriteRune(rs[i])
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inTok = true
		case r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			inTok = true
		case r == ' ' || r == '\t' || r == '\n':
			if inTok {
				out = append(out, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote")
	}
	if inTok {
		out = append(out, cur.String())
	}
	return out, nil
}

// EffectiveDockerfile returns the parsed Dockerfile and the targeted stage
// index of a built service.
func (f *File) EffectiveDockerfile(s *Service) (*Dockerfile, int, error) {
	if s.Build == nil {
		return nil, -1, fmt.Errorf("no build")
	}
	df, err := ParseDockerfile(f.DockerfilePath(s))
	if err != nil {
		return nil, -1, err
	}
	i, err := df.StageIndex(s.Build.Target)
	if err != nil {
		return nil, -1, err
	}
	return df, i, nil
}
