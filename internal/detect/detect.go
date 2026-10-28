// Package detect guesses, for each service of a compose file, which preset and
// runtime apply, by looking at images, commands and dependency files.
package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/presets"
)

// Result is the detection outcome for one service.
type Result struct {
	Service  string
	Preset   string
	Runtime  string // python, node or ""
	Port     int
	Reasons  []string // why this preset was chosen, shown to the user
	Warnings []string
}

// Deps summarises dependency files found in a build context.
type Deps struct {
	Python   map[string]bool // lower-cased distribution names
	Node     map[string]bool
	IsPython bool
	IsNode   bool
}

var (
	reqLineRE   = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-\[\]]+)`)
	bindPortRE  = regexp.MustCompile(`(?:--bind|-b|--port|-p|runserver)[ =]+(?:[0-9.]*:)?([0-9]{2,5})`)
	djangoCmdRE = regexp.MustCompile(`(manage\.py|wsgi|asgi|django)`)
)

// ReadDeps inspects requirements*.txt, pyproject.toml, Pipfile, setup.py and
// package.json in dir.
func ReadDeps(dir string) Deps {
	d := Deps{Python: map[string]bool{}, Node: map[string]bool{}}
	matches, _ := filepath.Glob(filepath.Join(dir, "requirements*.txt"))
	extra, _ := filepath.Glob(filepath.Join(dir, "requirements", "*.txt"))
	for _, f := range append(matches, extra...) {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		d.IsPython = true
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
				continue
			}
			if m := reqLineRE.FindStringSubmatch(line); m != nil {
				name := strings.ToLower(m[1])
				name = strings.SplitN(name, "[", 2)[0]
				d.Python[strings.ReplaceAll(name, "_", "-")] = true
			}
		}
	}
	for _, f := range []string{"pyproject.toml", "Pipfile", "setup.py", "setup.cfg", "uv.lock", "poetry.lock"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		d.IsPython = true
		low := strings.ToLower(string(data))
		for _, name := range []string{"django", "fastapi", "flask", "celery", "gunicorn", "uvicorn", "starlette"} {
			if regexp.MustCompile(`(^|[^a-z0-9-])` + name + `([^a-z0-9-]|$)`).MatchString(low) {
				d.Python[name] = true
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		d.IsNode = true
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			for k := range pkg.Dependencies {
				d.Node[k] = true
			}
		}
	}
	return d
}

// Service detects the preset of one compose service.
func Service(f *compose.File, s *compose.Service) Result {
	r := Result{Service: s.Name}
	img := strings.ToLower(s.Image)
	imgName := imageName(img)
	cmd := strings.ToLower(strings.Join(append(append([]string{}, s.Entrypoint...), s.Command...), " "))

	// Data stores first: recognised by image.
	switch {
	case strings.Contains(imgName, "postgres") || strings.Contains(imgName, "postgis") || strings.Contains(imgName, "timescale"):
		r.Preset, r.Port = "postgres", 5432
		r.Reasons = append(r.Reasons, "image "+s.Image)
		return r
	case strings.Contains(imgName, "redis") || strings.Contains(imgName, "valkey") || strings.Contains(imgName, "keydb"):
		r.Preset, r.Port = "redis", 6379
		r.Reasons = append(r.Reasons, "image "+s.Image)
		return r
	}

	deps := Deps{Python: map[string]bool{}, Node: map[string]bool{}}
	if s.Build != nil {
		deps = ReadDeps(f.ContextDir(s))
	}
	// Dockerfile CMD helps when compose has no command.
	if s.Build != nil {
		if df, i, err := f.EffectiveDockerfile(s); err == nil {
			if len(s.Command) == 0 {
				ep := s.Entrypoint
				if ep == nil {
					ep = df.Entrypoint(i)
				}
				cmd = strings.ToLower(strings.Join(append(append([]string{}, ep...), df.Cmd(i)...), " "))
			}
			if img == "" {
				imgName = imageName(strings.ToLower(df.ExternalBase(i)))
			}
		}
	}

	switch {
	case deps.IsPython || strings.HasPrefix(imgName, "python") || strings.Contains(cmd, "python") || strings.Contains(cmd, "gunicorn") || strings.Contains(cmd, "uvicorn"):
		r.Runtime = presets.RuntimePython
	case deps.IsNode || strings.HasPrefix(imgName, "node") || strings.HasPrefix(cmd, "node ") || strings.Contains(cmd, "npm ") || strings.Contains(cmd, "pnpm ") || strings.Contains(cmd, "yarn "):
		r.Runtime = presets.RuntimeNode
	}

	switch {
	case strings.Contains(cmd, "celery") && (strings.Contains(cmd, "worker") || strings.Contains(cmd, " beat")):
		r.Preset = "celery"
		r.Reasons = append(r.Reasons, "celery command")
		r.Runtime = presets.RuntimePython
	case r.Runtime == presets.RuntimePython && deps.Python["django"] && (len(cmd) == 0 || djangoCmdRE.MatchString(cmd) || strings.Contains(cmd, "gunicorn") || strings.Contains(cmd, "uvicorn")):
		r.Preset = "django"
		r.Reasons = append(r.Reasons, "django dependency")
	case r.Runtime == presets.RuntimePython && (deps.Python["fastapi"] || deps.Python["starlette"]):
		r.Preset = "fastapi"
		r.Reasons = append(r.Reasons, "fastapi dependency")
	case r.Runtime == presets.RuntimePython && strings.Contains(cmd, "manage.py"):
		r.Preset = "django"
		r.Reasons = append(r.Reasons, "manage.py command")
	default:
		r.Preset = "generic"
		switch r.Runtime {
		case presets.RuntimePython:
			r.Reasons = append(r.Reasons, "Python application with no recognized framework")
		case presets.RuntimeNode:
			r.Reasons = append(r.Reasons, "Node.js application")
		default:
			r.Reasons = append(r.Reasons, "no recognized technology")
		}
	}

	r.Port = guessPort(s, cmd)
	if r.Port == 0 && (r.Preset == "django" || r.Preset == "fastapi") {
		r.Port = 8000
		r.Warnings = append(r.Warnings, "port not found, assuming 8000")
	}
	if s.Build == nil && s.Image == "" {
		r.Warnings = append(r.Warnings, "neither image nor build: service skipped by instrumentation")
	}
	return r
}

// All runs detection on every service, sorted by name.
func All(f *compose.File) []Result {
	var out []Result
	for _, n := range f.ServiceNames() {
		out = append(out, Service(f, f.Services[n]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

func guessPort(s *compose.Service, cmd string) int {
	if m := bindPortRE.FindStringSubmatch(cmd); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	if p := s.ContainerPorts(); len(p) > 0 {
		return p[0]
	}
	return 0
}

// imageName strips the registry and tag: "docker.io/library/postgres:16" -> "postgres".
func imageName(img string) string {
	img = strings.SplitN(img, "@", 2)[0]
	if i := strings.LastIndex(img, "/"); i >= 0 {
		img = img[i+1:]
	}
	return strings.SplitN(img, ":", 2)[0]
}
