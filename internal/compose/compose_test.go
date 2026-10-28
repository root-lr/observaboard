package compose

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	cases := map[string][]string{
		`gunicorn app.wsgi -b 0.0.0.0:8000`:        {"gunicorn", "app.wsgi", "-b", "0.0.0.0:8000"},
		`sh -c "migrate && gunicorn x"`:            {"sh", "-c", "migrate && gunicorn x"},
		`echo 'a b' "c \"d\"" e\ f`:                {"echo", "a b", `c "d"`, "e f"},
		"celery -A shop worker\n  --loglevel=info": {"celery", "-A", "shop", "worker", "--loglevel=info"},
	}
	for in, want := range cases {
		got, err := SplitWords(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if _, err := SplitWords(`echo "oops`); err == nil {
		t.Error("expected unclosed quote error")
	}
}

func TestParseDockerfile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Dockerfile")
	os.WriteFile(p, []byte(`FROM node:22 AS build
USER node
CMD ["npm", "run", "build"]

FROM --platform=linux/amd64 python:3.12-slim
# comment
RUN apt-get update \
 && apt-get install -y curl
USER app
ENTRYPOINT ["/entrypoint.sh"]
CMD gunicorn app.wsgi --bind 0.0.0.0:8000
`), 0o644)
	d, err := ParseDockerfile(p)
	if err != nil {
		t.Fatal(err)
	}
	i, _ := d.StageIndex("")
	if d.ExternalBase(i) != "python:3.12-slim" || d.User(i) != "app" {
		t.Errorf("base=%q user=%q", d.ExternalBase(i), d.User(i))
	}
	if !reflect.DeepEqual(d.Cmd(i), []string{"/bin/sh", "-c", "gunicorn app.wsgi --bind 0.0.0.0:8000"}) {
		t.Errorf("cmd=%q", d.Cmd(i))
	}
	if !reflect.DeepEqual(d.Entrypoint(i), []string{"/entrypoint.sh"}) {
		t.Errorf("entrypoint=%q", d.Entrypoint(i))
	}
}

func TestDockerfileStagesAndInheritance(t *testing.T) {
	d := ParseDockerfileString(`ARG V=3.12
FROM python:${V} AS base
USER app
CMD ["gunicorn", "x"]

FROM base AS runtime
RUN echo runtime

FROM runtime AS debug
RUN pip install debugpy
`)
	if len(d.Stages) != 3 {
		t.Fatalf("stages: %+v", d.Stages)
	}
	i, err := d.StageIndex("RUNTIME")
	if err != nil || i != 1 {
		t.Fatalf("target: %d %v", i, err)
	}
	if d.User(i) != "app" || d.Cmd(i)[0] != "gunicorn" || d.ExternalBase(i) != "python:${V}" {
		t.Errorf("inheritance: user=%q cmd=%q base=%q", d.User(i), d.Cmd(i), d.ExternalBase(i))
	}
	out := d.InsertAfterStage(i, "# BLOCK")
	if !strings.Contains(out, "RUN echo runtime\n\n# BLOCK\n\nFROM runtime AS debug") && !strings.Contains(out, "RUN echo runtime\n# BLOCK\n\nFROM runtime AS debug") {
		t.Errorf("block not inserted in the runtime stage:\n%s", out)
	}
	if _, err := d.StageIndex("nope"); err == nil {
		t.Error("unknown target accepted")
	}
}

func TestLoadCompose(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(`
x-env: &env
  A: "1"
services:
  api:
    build:
      context: ./src
      dockerfile: docker/Dockerfile
    command: ["python", "app.py"]
    environment: *env
    networks: [backend]
    ports:
      - target: 8080
        published: 80
  db:
    image: postgres
    environment:
      - POSTGRES_PASSWORD=x
`), 0o644)
	if got := Find(dir); filepath.Base(got) != "docker-compose.yml" {
		t.Fatalf("Find = %q", got)
	}
	f, err := Load(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	api := f.Services["api"]
	if api.Build.Context != "./src" || f.DockerfilePath(api) != filepath.Join(dir, "src", "docker", "Dockerfile") {
		t.Errorf("build: %+v %s", api.Build, f.DockerfilePath(api))
	}
	if api.Environment["A"] != "1" || !reflect.DeepEqual(api.Networks, []string{"backend"}) {
		t.Errorf("env/networks: %v %v", api.Environment, api.Networks)
	}
	if p := api.ContainerPorts(); len(p) != 1 || p[0] != 8080 {
		t.Errorf("ports: %v", p)
	}
	if f.Services["db"].Environment["POSTGRES_PASSWORD"] != "x" {
		t.Errorf("list env not parsed")
	}
}
