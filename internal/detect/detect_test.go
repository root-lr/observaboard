package detect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/root-lr/observaboard/internal/compose"
)

func TestDetectExampleShop(t *testing.T) {
	f, err := compose.Load(filepath.Join("..", "..", "examples", "django-shop", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		preset, runtime string
		port            int
	}{
		"web":     {"django", "python", 8000},
		"worker":  {"celery", "python", 0},
		"db":      {"postgres", "", 5432},
		"redis":   {"redis", "", 6379},
		"traffic": {"generic", "python", 0},
	}
	for _, r := range All(f) {
		w, ok := want[r.Service]
		if !ok {
			t.Errorf("unexpected service %s", r.Service)
			continue
		}
		if r.Preset != w.preset || r.Runtime != w.runtime || r.Port != w.port {
			t.Errorf("%s: got preset=%s runtime=%s port=%d, want %+v", r.Service, r.Preset, r.Runtime, r.Port, w)
		}
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectFrameworks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "compose.yaml", `
services:
  api:
    build: ./api
    ports: ["9000:8080"]
  front:
    build: ./front
  admin:
    build: ./admin
  cache:
    image: valkey/valkey:8
  pg:
    image: docker.io/library/postgres:16@sha256:abc
`)
	write(t, dir, "api/requirements.txt", "fastapi==0.115\nuvicorn[standard]\n")
	write(t, dir, "api/Dockerfile", "FROM python:3.12\nCMD [\"uvicorn\", \"main:app\", \"--port\", \"8080\"]\n")
	write(t, dir, "front/package.json", `{"dependencies":{"next":"15"}}`)
	write(t, dir, "front/Dockerfile", "FROM node:22\nCMD npm start\n")
	write(t, dir, "admin/pyproject.toml", "[project]\ndependencies = [\"Django>=5\", \"gunicorn\"]\n")
	write(t, dir, "admin/Dockerfile", "FROM python:3.12\nCMD gunicorn admin.wsgi -b 0.0.0.0:8001\n")
	f, err := compose.Load(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Result{}
	for _, r := range All(f) {
		got[r.Service] = r
	}
	check := func(svc, preset, runtime string, port int) {
		t.Helper()
		r := got[svc]
		if r.Preset != preset || r.Runtime != runtime || r.Port != port {
			t.Errorf("%s: got %s/%s/%d, want %s/%s/%d", svc, r.Preset, r.Runtime, r.Port, preset, runtime, port)
		}
	}
	check("api", "fastapi", "python", 8080)
	check("front", "generic", "node", 0)
	check("admin", "django", "python", 8001)
	check("cache", "redis", "", 6379)
	check("pg", "postgres", "", 5432)
}
