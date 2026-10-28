package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/root-lr/observaboard/internal/agent"
	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/detect"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/initgen"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
)

func runInit(args []string) error {
	fs := newFlags("observa init", "observa init [--project <name>] [--env prod] [--yes] [--runtime docker --service api=django ...]")
	dirFlag := fs.String("dir", ".", "project directory")
	projectName := fs.String("project", "", "project name (default: directory name or existing value)")
	env := fs.String("env", "", "environment (default: prod or existing value)")
	yes := fs.Bool("yes", false, "accept detected choices without prompting")
	runtime := fs.String("runtime", "auto", "compose (compose file) or docker (containers started with docker run); auto = compose if present")
	var services multiFlag
	fs.Var(&services, "service", "docker mode: service to declare, name=preset[:python|node] (repeatable)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	dir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, project.FileName)

	// Start from the existing observa.yaml so manual edits survive a re-run.
	cfg := &project.Config{Version: 1, Services: map[string]*project.Service{}}
	existing := false
	if fileExists(cfgPath) {
		c, err := project.Load(cfgPath)
		if err != nil {
			var ve project.ValidationError
			if !errors.As(err, &ve) {
				return err
			}
			warn("existing observa.yaml is invalid; errors will be fixed where possible")
		}
		if c != nil {
			cfg = c
			existing = true
			if cfg.Services == nil {
				cfg.Services = map[string]*project.Service{}
			}
		}
	}
	if *projectName != "" {
		cfg.Project = *projectName
	}
	if cfg.Project == "" {
		cfg.Project = ask("Project name?", slugify(filepath.Base(dir)), *yes)
	}
	if *env != "" {
		cfg.Env = *env
	}
	if cfg.Env == "" {
		cfg.Env = ask("Environment?", "prod", *yes)
	}

	composePath := compose.Find(dir)
	mode := *runtime
	if mode == "auto" {
		mode = "compose"
		if composePath == "" {
			mode = "docker"
		}
	}

	switch mode {
	case "compose":
		if composePath == "" {
			return fmt.Errorf("no compose file in %s (compose.yaml, docker-compose.yml…); for docker run containers: --runtime docker --service name=preset", dir)
		}
		f, err := compose.Load(composePath)
		if err != nil {
			return err
		}
		step("Analyzing %s", filepath.Base(composePath))
		fmt.Fprintf(stdout, "\n%-16s %-10s %-8s %-6s %s\n", "SERVICE", "PRESET", "RUNTIME", "PORT", "DETECTED BY")
		ignored := map[string]bool{}
		for _, n := range cfg.Ignored {
			ignored[n] = true
		}
		for _, r := range detect.All(f) {
			if ignored[r.Service] {
				fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6s %s\n", r.Service, "-", "-", "-", "ignored (observa.yaml: ignored)")
				continue
			}
			if prev, ok := cfg.Services[r.Service]; ok {
				fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6d %s\n", r.Service, prev.Preset, prev.EffectiveRuntime(), prev.EffectivePort(), "existing observa.yaml (kept)")
				continue
			}
			fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6d %s\n", r.Service, r.Preset, dash(r.Runtime), r.Port, strings.Join(r.Reasons, ", "))
			for _, w := range r.Warnings {
				info("  ! %s", w)
			}
			cfg.Services[r.Service] = &project.Service{Preset: r.Preset, Runtime: r.Runtime, Port: r.Port}
		}
		fmt.Fprintln(stdout)
		if !*yes {
			for _, name := range cfg.ServiceNames() {
				if _, inCompose := f.Services[name]; !inCompose {
					continue
				}
				s := cfg.Services[name]
				answer := ask(fmt.Sprintf("Preset for %s (%s, or \"ignore\")?", name, strings.Join(presets.Names(), "/")), s.Preset, false)
				if answer == "ignore" || answer == "-" {
					delete(cfg.Services, name)
					cfg.Ignored = append(cfg.Ignored, name)
					continue
				}
				s.Preset = answer
			}
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			return project.ValidationError(errs)
		}
		plan, err := initgen.FromCompose(cfg, f)
		if err != nil {
			return err
		}
		return writeInit(dir, cfgPath, cfg, plan, existing, initgen.ComposeCommand(f))

	case "docker":
		for _, sv := range services {
			name, spec, okCut := strings.Cut(sv, "=")
			if !okCut {
				return usagef("--service expects name=preset[:runtime] (e.g. api=django)")
			}
			preset, rt, _ := strings.Cut(spec, ":")
			s := cfg.Services[name]
			if s == nil {
				s = &project.Service{}
				cfg.Services[name] = s
			}
			s.Preset, s.Runtime = preset, rt
		}
		if len(cfg.Services) == 0 {
			return usagef("docker mode: declare services with --service name=preset (e.g. --service api=django --service db=postgres)")
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			return project.ValidationError(errs)
		}
		plan := initgen.ForDockerRun(cfg)
		return writeInit(dir, cfgPath, cfg, plan, existing, "")
	default:
		return usagef("--runtime: compose, docker or auto")
	}
}

func writeInit(dir, cfgPath string, cfg *project.Config, plan *initgen.Plan, existing bool, composeName string) error { // composeName: full docker compose command, "" in docker run mode
	if err := cfg.Save(cfgPath); err != nil {
		return err
	}
	if existing {
		ok("observa.yaml updated")
	} else {
		ok("observa.yaml created")
	}
	written, err := plan.Write(dir)
	if err != nil {
		return err
	}
	for _, w := range written {
		ok("%s generated", w)
	}
	for _, n := range plan.Notes {
		warn("%s", n)
	}
	fmt.Fprintln(stdout)
	info("Next steps, on the server running the project (with the agent installed):")
	info("  1. observa apply                      # registers thresholds, routing and databases with the agent")
	if composeName != "" {
		info("  2. %s up -d --build", composeName)
	} else {
		info("  2. restart each container with the docker run command shown above")
	}
	info("  3. observa doctor                     # checks that data is arriving")
	info("Tune thresholds and notifications in observa.yaml (services.<name>.alerts, notifications).")
	return nil
}

func runValidate(args []string) error {
	fs := newFlags("observa validate", "observa validate [observa.yaml]")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	path := project.FileName
	if len(pos) > 0 {
		path = pos[0]
	}
	cfg, err := project.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project.ErrNotFound
		}
		return err
	}
	// Cross-check with the compose file when there is one.
	if cp := compose.Find(filepath.Dir(path)); cp != "" {
		if f, err := compose.Load(cp); err == nil {
			for _, name := range cfg.ServiceNames() {
				if _, ok := f.Services[name]; !ok && cfg.Services[name].Container == "" {
					warn("services.%s missing from %s (started with docker run? set container:)", name, filepath.Base(cp))
				}
			}
		}
	}
	ok("%s valid: project %s, env %s, %d service(s)", path, cfg.Project, cfg.Env, len(cfg.Services))
	return nil
}

func runApply(args []string) error {
	fs := newFlags("observa apply", "observa apply [observa.yaml] [--agent-dir <directory>] [--remove]")
	dir := agentDirFlag(fs, "agent-dir")
	remove := fs.Bool("remove", false, "remove this project from the agent")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	path := project.FileName
	if len(pos) > 0 {
		path = pos[0]
	}
	cfg, err := project.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project.ErrNotFound
		}
		return err
	}
	d := agent.Dir{Path: *dir}
	if _, err := d.Settings(); err != nil {
		return err
	}
	if *remove {
		if err := d.RemoveProject(cfg.Project, cfg.Env); err != nil {
			return err
		}
		if err := reloadAgent(d); err != nil {
			return err
		}
		ok("Project %s/%s removed from the agent", cfg.Project, cfg.Env)
		return nil
	}
	if err := d.SaveProject(cfg); err != nil {
		return err
	}
	ok("Project %s/%s registered with the agent (%s)", cfg.Project, cfg.Env, d.Path)
	for _, name := range cfg.ServiceNames() {
		s := cfg.Services[name]
		p, _ := presets.Get(s.Preset)
		var th []string
		for _, param := range p.Params {
			v, custom := s.Alerts[param.Key]
			if !custom {
				v = param.Default
			}
			mark := ""
			if custom {
				mark = "*"
			}
			th = append(th, fmt.Sprintf("%s=%g%s", param.Key, v, mark))
		}
		info("%-14s %-9s %-5s %s", name, s.Preset, s.EffectiveInstrumentation(), strings.Join(th, " "))
	}
	if len(cfg.Notifications.Critical)+len(cfg.Notifications.Warning) > 0 {
		info("Notifications: critical → %s · warning → %s (channels defined in central.yaml)",
			dash(strings.Join(cfg.Notifications.Critical, ",")), dash(strings.Join(cfg.Notifications.Warning, ",")))
	}
	info("(* = custom threshold; other values are preset defaults)")
	if err := reloadAgent(d); err != nil {
		return err
	}
	if out, err := dockerx.Output("ps", "--filter", "label=observa.project="+cfg.Project, "--filter", "label=observa.env="+cfg.Env, "--format", "{{.Names}}"); err == nil && strings.TrimSpace(out) == "" {
		warn("No container of %s/%s is running with observa labels yet: start it with the override generated by observa init.", cfg.Project, cfg.Env)
	}
	return nil
}

func runPresetList(args []string) error {
	fmt.Fprintf(stdout, "%-10s %-12s %s\n", "PRESET", "DASHBOARD", "DESCRIPTION")
	for _, p := range presets.All() {
		fmt.Fprintf(stdout, "%-10s %-12s %s\n", p.Name, strings.TrimPrefix(p.Dashboard, "observa-"), p.Description)
	}
	return nil
}

func runPresetShow(args []string) error {
	if len(args) != 1 {
		return usagef("observa preset show <name> (%s)", strings.Join(presets.Names(), ", "))
	}
	p, ok := presets.Get(args[0])
	if !ok {
		return usagef("unknown preset %q (%s)", args[0], strings.Join(presets.Names(), ", "))
	}
	fmt.Fprint(stdout, p.Describe())
	return nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "my-project"
	}
	return s
}
