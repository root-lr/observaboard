package cli

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/root-lr/observaboard/examples"
	"github.com/root-lr/observaboard/internal/agent"
	"github.com/root-lr/observaboard/internal/central"
	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/detect"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/initgen"
	"github.com/root-lr/observaboard/internal/project"
)

const localAgentName = "local"

func defaultLocalDir() string {
	if v := os.Getenv("OBSERVA_LOCAL_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".observa-local"
	}
	return filepath.Join(home, ".observa", "local")
}

func runUp(args []string) error {
	fs := newFlags("observa up", "observa up --all-in-one [--with-example] [--listen 127.0.0.1:3000]")
	allInOne := fs.Bool("all-in-one", false, "central and agent on this machine (dev workstation, testing, small single server)")
	dir := fs.String("dir", defaultLocalDir(), "all-in-one install directory")
	listen := fs.String("listen", "127.0.0.1:3000", "address Grafana is published on (0.0.0.0:3000 to expose it on the network)")
	withExample := fs.Bool("with-example", false, "also run the example Django app with generated traffic")
	ebpf := fs.Bool("ebpf", false, "enable eBPF on the agent (Linux)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if !*allInOne {
		fs.Usage()
		return usagef("up only supports --all-in-one; for separate servers: observa central install, then observa agent install")
	}
	if err := dockerx.Available(); err != nil {
		return err
	}
	cd := central.Dir{Path: filepath.Join(*dir, "central")}
	ad := agent.Dir{Path: filepath.Join(*dir, "agent")}

	var s *central.Settings
	if cd.Exists() {
		var err error
		if s, err = cd.Settings(); err != nil {
			return err
		}
		if fsFlagSet(args, "listen") {
			s.Listen = *listen
		}
	} else {
		s = central.Defaults(central.ModeLocal)
		s.Listen = *listen
	}
	if errs := s.Validate(); len(errs) > 0 {
		return usagef("%s", strings.Join(errs, "; "))
	}
	if err := requireWritable(cd.Path); err != nil {
		return err
	}
	if err := cd.SaveSettings(s); err != nil {
		return err
	}

	// Reuse the local agent token when both sides still have it.
	token := ""
	env, _ := ad.Env()
	tokens, err := cd.Tokens()
	if err != nil {
		return err
	}
	hasLocal := false
	for _, t := range tokens {
		hasLocal = hasLocal || t.Name == localAgentName
	}
	if hasLocal && env["OBSERVA_TOKEN"] != "" {
		token = localAgentName + "." + env["OBSERVA_TOKEN"]
	} else {
		if hasLocal {
			if err := cd.RevokeToken(localAgentName); err != nil {
				return err
			}
		}
		if token, err = cd.CreateToken(localAgentName); err != nil {
			return err
		}
	}

	step("Central")
	if err := installCentral(cd, s, "", false); err != nil {
		return err
	}
	if err := waitHTTP(s.BaseURL()+"/ingest/ping", 60*time.Second); err != nil {
		warn("Caddy is not responding yet on %s: %v", s.BaseURL(), err)
	}
	fmt.Fprintln(stdout)
	step("Agent")
	if err := installAgent(agentInstallOpts{
		dir:        ad.Path,
		centralURL: "http://observa-caddy:3000", // shared `observa` network
		token:      token,
		host:       defaultAgentName(),
		ebpf:       *ebpf,
		pingURL:    s.BaseURL(),
	}); err != nil {
		return err
	}

	if *withExample {
		fmt.Fprintln(stdout)
		step("Example app (Django + PostgreSQL + Redis + Celery + traffic)")
		if err := startExample(filepath.Join(*dir, "example"), ad); err != nil {
			return err
		}
	}

	fmt.Fprintln(stdout)
	ok("ObservaBoard is running on this machine")
	info("Grafana: %s  (user %s, password in %s)", s.BaseURL(), s.AdminUser, filepath.Join(cd.Path, ".env"))
	info("Runbooks: %s/runbooks/", s.BaseURL())
	if *withExample {
		info("Example shop: http://localhost:8000 — data shows up in Grafana within 1 to 2 minutes.")
		info("Simulate an incident: docker compose --project-directory %s exec web python manage.py chaos errors", filepath.Join(*dir, "example"))
	}
	info("Stop: observa down --all-in-one")
	return nil
}

// fsFlagSet reports whether a flag was given explicitly.
func fsFlagSet(args []string, name string) bool {
	for _, a := range args {
		if a == "--"+name || a == "-"+name || strings.HasPrefix(a, "--"+name+"=") || strings.HasPrefix(a, "-"+name+"=") {
			return true
		}
	}
	return false
}

func waitHTTP(url string, timeout time.Duration) error {
	if dockerx.DryRun {
		return nil
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var last error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil // any HTTP answer (even 401) means Caddy is up
		}
		last = err
		time.Sleep(2 * time.Second)
	}
	return last
}

// startExample writes the example shop, instruments it with the same code as
// `observa init`, registers it with the agent and starts it.
func startExample(dir string, ad agent.Dir) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	err := fs.WalkDir(examples.FS, "django-shop", func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "django-shop"), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if e.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := examples.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return err
	}
	cp := compose.Find(dir)
	f, err := compose.Load(cp)
	if err != nil {
		return err
	}
	cfg := &project.Config{Version: 1, Project: "boutique", Env: "local", Services: map[string]*project.Service{}}
	for _, r := range detect.All(f) {
		if r.Service == "traffic" {
			continue // load generator: not part of the monitored app
		}
		cfg.Services[r.Service] = &project.Service{Preset: r.Preset, Runtime: r.Runtime, Port: r.Port}
	}
	if s := cfg.Services["web"]; s != nil {
		s.Healthcheck = "/health/"
		s.Alerts = map[string]float64{"latency_p95_ms": 800}
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return project.ValidationError(errs)
	}
	plan, err := initgen.FromCompose(cfg, f)
	if err != nil {
		return err
	}
	if err := cfg.Save(filepath.Join(dir, project.FileName)); err != nil {
		return err
	}
	if _, err := plan.Write(dir); err != nil {
		return err
	}
	for _, name := range cfg.ServiceNames() {
		info("%-8s preset %-9s instrumentation %s", name, cfg.Services[name].Preset, cfg.Services[name].EffectiveInstrumentation())
	}
	if s := cfg.Services["db"]; s != nil {
		dsn := fmt.Sprintf("postgresql://shop:shop@%s:5432/shop?sslmode=disable", project.Alias(cfg.Project, cfg.Env, "db"))
		if err := ad.SetEnv(map[string]string{cfg.EffectiveSecretEnv("db"): dsn}); err != nil {
			return err
		}
	}
	if err := ad.SaveProject(cfg); err != nil {
		return err
	}
	if err := renderAndRecreate(ad); err != nil { // new .env secret + exporters
		return err
	}
	step("Building and starting the example shop (first build takes a few minutes)")
	return dockerx.Run("compose", "--project-directory", dir, "-f", cp, "-f", filepath.Join(dir, initgen.OverrideFileName), "up", "-d", "--build")
}

func runDown(args []string) error {
	fs := newFlags("observa down", "observa down --all-in-one [--volumes]")
	allInOne := fs.Bool("all-in-one", false, "stop the all-in-one install")
	dir := fs.String("dir", defaultLocalDir(), "all-in-one install directory")
	volumes := fs.Bool("volumes", false, "also delete data (metrics, logs, traces, Grafana)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	if !*allInOne {
		return usagef("usage: observa down --all-in-one; for a server: docker compose --project-directory <directory> down")
	}
	down := []string{"down", "--remove-orphans"}
	if *volumes {
		down = append(down, "--volumes")
	}
	ex := filepath.Join(*dir, "example")
	if cp := compose.Find(ex); cp != "" {
		step("Stopping the example shop")
		args := []string{"compose", "--project-directory", ex, "-f", cp}
		if fileExists(filepath.Join(ex, initgen.OverrideFileName)) {
			args = append(args, "-f", filepath.Join(ex, initgen.OverrideFileName))
		}
		if err := dockerx.Run(append(args, down...)...); err != nil {
			warn("%v", err)
		}
	}
	for _, sub := range []string{"agent", "central"} {
		p := filepath.Join(*dir, sub)
		if fileExists(filepath.Join(p, "docker-compose.yml")) {
			step("Stopping %s", sub)
			if err := dockerx.Compose(p, down...); err != nil {
				warn("%v", err)
			}
		}
	}
	ok("Stopped%s", map[bool]string{true: " (data deleted)", false: " (data kept)"}[*volumes])
	return nil
}
