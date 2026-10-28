package initgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/detect"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

func exampleCfg(t *testing.T) (*project.Config, *compose.File) {
	t.Helper()
	f, err := compose.Load(filepath.Join("..", "..", "examples", "django-shop", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{Version: 1, Project: "boutique", Env: "prod", Services: map[string]*project.Service{}}
	for _, r := range detect.All(f) {
		cfg.Services[r.Service] = &project.Service{Preset: r.Preset, Runtime: r.Runtime, Port: r.Port}
	}
	cfg.Services["web"].Healthcheck = "/health/"
	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatal(errs)
	}
	return cfg, f
}

type overrideFile struct {
	Services map[string]struct {
		Labels      map[string]string         `yaml:"labels"`
		Environment map[string]string         `yaml:"environment"`
		Command     []string                  `yaml:"command"`
		Image       string                    `yaml:"image"`
		Build       map[string]string         `yaml:"build"`
		Networks    map[string]map[string]any `yaml:"networks"`
	} `yaml:"services"`
	Networks map[string]map[string]any `yaml:"networks"`
}

func TestFromComposeExample(t *testing.T) {
	cfg, f := exampleCfg(t)
	plan, err := FromCompose(cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, gf := range plan.Files {
		files[gf.Path] = string(gf.Content)
	}
	var o overrideFile
	if err := yaml.Unmarshal([]byte(files[OverrideFileName]), &o); err != nil {
		t.Fatalf("override is not valid YAML: %v\n%s", err, files[OverrideFileName])
	}
	web := o.Services["web"]
	if web.Labels["observa.enable"] != "true" || web.Labels["observa.preset"] != "django" || web.Labels["observa.service"] != "web" {
		t.Errorf("web labels: %v", web.Labels)
	}
	if len(web.Command) < 4 || web.Command[0] != "opentelemetry-instrument" || web.Command[1] != "sh" || web.Command[2] != "-c" {
		t.Errorf("web command not wrapped: %q", web.Command)
	}
	if web.Environment["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/protobuf" || web.Environment["OTEL_METRICS_EXPORTER"] != "none" {
		t.Errorf("web env: %v", web.Environment)
	}
	if !strings.Contains(web.Environment["OTEL_RESOURCE_ATTRIBUTES"], "observa.project=boutique") ||
		web.Environment["OTEL_PYTHON_EXCLUDED_URLS"] != "health" {
		t.Errorf("web resource/excluded: %v", web.Environment)
	}
	if web.Build["dockerfile"] != "Dockerfile.observa" || web.Image != "observa-local/boutique-web:prod" {
		t.Errorf("web build/image: %v %s", web.Build, web.Image)
	}
	if _, ok := web.Networks["default"]; !ok {
		t.Errorf("web must stay on the default network: %v", web.Networks)
	}
	if al, _ := web.Networks["observa"]["aliases"].([]any); len(al) != 1 || al[0] != "boutique-prod-web" {
		t.Errorf("web alias: %v", web.Networks["observa"])
	}
	worker := o.Services["worker"]
	if strings.Join(worker.Command, " ") != "opentelemetry-instrument celery -A shop worker --loglevel=info --concurrency=2" {
		t.Errorf("worker command: %q", worker.Command)
	}
	db := o.Services["db"]
	if db.Build != nil || db.Environment != nil || db.Command != nil {
		t.Errorf("db must only get labels and network: %+v", db)
	}
	if o.Networks["observa"]["external"] != true {
		t.Errorf("observa network must be external: %v", o.Networks)
	}
	df := files["Dockerfile.observa"]
	if !strings.Contains(df, "opentelemetry-bootstrap -a install") || !strings.HasSuffix(strings.TrimSpace(df), "USER shop") || !strings.Contains(df, "USER root") {
		t.Errorf("derived Dockerfile:\n%s", df)
	}
}

func TestImageOnlyServiceGetsDerivedImage(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "compose.yaml")
	writeFile(t, cp, "services:\n  api:\n    image: ghcr.io/acme/api:1.2\n    command: gunicorn api.wsgi\n")
	f, err := compose.Load(cp)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{Version: 1, Project: "acme", Env: "prod", Services: map[string]*project.Service{
		"api": {Preset: "django", Runtime: "python", Port: 8000},
	}}
	plan, err := FromCompose(cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, gf := range plan.Files {
		if gf.Path == filepath.Join(".observa", "api", "Dockerfile") {
			found = strings.HasPrefix(strings.SplitN(string(gf.Content), "\n", 3)[1], "FROM ghcr.io/acme/api:1.2")
		}
	}
	if !found {
		t.Fatalf("expected .observa/api/Dockerfile deriving from the image, got %+v", plan.Files)
	}
}

func TestDockerRunPlan(t *testing.T) {
	cfg := &project.Config{Version: 1, Project: "acme", Env: "prod", Services: map[string]*project.Service{
		"api": {Preset: "fastapi", Runtime: "python"},
		"db":  {Preset: "postgres"},
	}}
	plan := ForDockerRun(cfg)
	var env string
	for _, gf := range plan.Files {
		if gf.Path == filepath.Join(".observa", "api.env") {
			env = string(gf.Content)
		}
	}
	if !strings.Contains(env, "OTEL_SERVICE_NAME=api") {
		t.Fatalf("api.env missing or wrong:\n%s", env)
	}
	joined := strings.Join(plan.Notes, "\n")
	if !strings.Contains(joined, "--network observa") || !strings.Contains(joined, "-l observa.preset=postgres") {
		t.Fatalf("docker run hints:\n%s", joined)
	}
}
