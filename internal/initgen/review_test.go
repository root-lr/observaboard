package initgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

func init() {
	// Tests never call docker.
	ImageUser = func(image string) string {
		if strings.HasPrefix(image, "nonroot/") {
			return "1001"
		}
		return ""
	}
}

type genResult struct {
	override overrideFile
	raw      string
	files    map[string]string
	notes    string
}

func generate(t *testing.T, files map[string]string, services map[string]*project.Service) genResult {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		writeFile(t, full, c)
	}
	f, err := compose.Load(compose.Find(dir))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &project.Config{Version: 1, Project: "acme", Env: "prod", Services: services}
	if errs := cfg.Validate(); len(errs) > 0 {
		t.Fatal(errs)
	}
	plan, err := FromCompose(cfg, f)
	if err != nil {
		t.Fatal(err)
	}
	r := genResult{files: map[string]string{}, notes: strings.Join(plan.Notes, "\n")}
	for _, gf := range plan.Files {
		r.files[gf.Path] = string(gf.Content)
	}
	r.raw = r.files[OverrideFileName]
	if err := yaml.Unmarshal([]byte(r.raw), &r.override); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNetworkModeHostGetsNoNetworks(t *testing.T) {
	r := generate(t, map[string]string{"compose.yaml": "services:\n  agent:\n    image: nginx\n    network_mode: host\n"},
		map[string]*project.Service{"agent": {Preset: "generic"}})
	if strings.Contains(r.raw, "networks:") {
		t.Fatalf("networks added to a network_mode: host service:\n%s", r.raw)
	}
	if !strings.Contains(r.notes, "network_mode") {
		t.Errorf("missing note: %s", r.notes)
	}
}

func TestMultiStageTargetAndInheritedUser(t *testing.T) {
	r := generate(t, map[string]string{
		"compose.yaml": "services:\n  api:\n    build: {context: ., target: runtime}\n",
		"Dockerfile":   "FROM python:3.12 AS base\nUSER app\nCMD [\"gunicorn\", \"api.wsgi\"]\n\nFROM base AS runtime\nRUN echo ok\n\nFROM runtime AS debug\nRUN pip install debugpy\n",
	}, map[string]*project.Service{"api": {Preset: "django", Runtime: "python"}})
	df := r.files["Dockerfile.observa"]
	runtimePart := df[strings.Index(df, "FROM base AS runtime"):strings.Index(df, "FROM runtime AS debug")]
	if !strings.Contains(runtimePart, "opentelemetry-bootstrap") || !strings.Contains(runtimePart, "USER root") || !strings.Contains(runtimePart, "USER app") {
		t.Fatalf("SDK not installed in the target stage with user handling:\n%s", df)
	}
	if r.override.Services["api"].Build["target"] != "runtime" {
		t.Errorf("target lost: %v", r.override.Services["api"].Build)
	}
	if strings.Join(r.override.Services["api"].Command, " ") != "opentelemetry-instrument gunicorn api.wsgi" {
		t.Errorf("command: %q", r.override.Services["api"].Command)
	}
}

func TestEntrypointIsWrappedNotCommand(t *testing.T) {
	r := generate(t, map[string]string{
		"compose.yaml": "services:\n  w:\n    build: .\n    entrypoint: [python]\n    command: [worker.py]\n  g:\n    build: ./g\n",
		"Dockerfile":   "FROM python:3.12\n",
		"g/Dockerfile": "FROM python:3.12\nENTRYPOINT [\"/docker-entrypoint.sh\"]\nCMD [\"gunicorn\", \"app:app\"]\n",
	}, map[string]*project.Service{"w": {Preset: "generic", Runtime: "python"}, "g": {Preset: "fastapi"}})
	var w struct {
		Entrypoint []string `yaml:"entrypoint"`
		Command    []string `yaml:"command"`
	}
	var full struct {
		Services map[string]struct {
			Entrypoint []string `yaml:"entrypoint"`
			Command    []string `yaml:"command"`
		} `yaml:"services"`
	}
	yaml.Unmarshal([]byte(r.raw), &full)
	w = full.Services["w"]
	if strings.Join(w.Entrypoint, " ") != "opentelemetry-instrument python" || strings.Join(w.Command, " ") != "worker.py" {
		t.Errorf("w: entrypoint=%q command=%q", w.Entrypoint, w.Command)
	}
	g := full.Services["g"]
	if strings.Join(g.Entrypoint, " ") != "opentelemetry-instrument /docker-entrypoint.sh" || strings.Join(g.Command, " ") != "gunicorn app:app" {
		t.Errorf("g: entrypoint=%q command=%q", g.Entrypoint, g.Command)
	}
}

func TestDollarFromDockerfileIsEscaped(t *testing.T) {
	r := generate(t, map[string]string{
		"compose.yaml": "services:\n  api:\n    build: .\n  api2:\n    build: .\n    command: gunicorn x --bind 0.0.0.0:${PORT}\n",
		"Dockerfile":   "FROM python:3.12\nCMD gunicorn app.wsgi --bind 0.0.0.0:$PORT\n",
	}, map[string]*project.Service{"api": {Preset: "django", Runtime: "python"}, "api2": {Preset: "django", Runtime: "python"}})
	if got := r.override.Services["api"].Command; got[len(got)-1] != "gunicorn app.wsgi --bind 0.0.0.0:$$PORT" {
		t.Errorf("Dockerfile $ not escaped: %q", got)
	}
	if got := r.override.Services["api2"].Command; got[len(got)-1] != "0.0.0.0:${PORT}" {
		t.Errorf("compose-file value must stay as written: %q", got)
	}
}

func TestImageOnlyNonRootUser(t *testing.T) {
	r := generate(t, map[string]string{"compose.yaml": "services:\n  api:\n    image: nonroot/api:1\n    command: gunicorn api.wsgi\n"},
		map[string]*project.Service{"api": {Preset: "django", Runtime: "python"}})
	df := r.files[filepath.Join(".observa", "api", "Dockerfile")]
	if !strings.Contains(df, "USER root") || !strings.HasSuffix(strings.TrimSpace(df), "USER 1001") {
		t.Errorf("derived Dockerfile must switch to root and back:\n%s", df)
	}
}

func TestUnderscoreServiceAndOverrideCommand(t *testing.T) {
	r := generate(t, map[string]string{
		"docker-compose.yml":          "services:\n  celery_worker:\n    image: python:3.12\n    command: celery -A x worker\n",
		"docker-compose.override.yml": "services:\n  celery_worker:\n    environment: {DEBUG: '1'}\n",
	}, map[string]*project.Service{"celery_worker": {Preset: "celery"}})
	if !strings.Contains(r.raw, "acme-prod-celery-worker") || !strings.Contains(r.raw, "observa-local/acme-celery-worker:prod") {
		t.Errorf("alias/image not slugified:\n%s", r.raw)
	}
	if !strings.Contains(r.raw, "-f docker-compose.yml -f docker-compose.override.yml -f docker-compose.observa.yml") {
		t.Errorf("usage line must keep the implicit override:\n%s", r.raw)
	}
}
