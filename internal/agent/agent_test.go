package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/internal/project"
)

func newAgent(t *testing.T, s *Settings) Dir {
	t.Helper()
	d := Dir{Path: t.TempDir()}
	if err := d.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRenderFullConfig(t *testing.T) {
	d := newAgent(t, &Settings{Name: "srv-a", Host: "srv-a", CentralURL: "https://observa.example.com", EBPF: true, TLS: TLS{CAFile: true}})
	cfg, err := project.Parse([]byte(`
version: 1
project: wulo
env: prod
services:
  api: {preset: django, alerts: {latency_p95_ms: 750}}
  front: {preset: generic, instrumentation: ebpf, port: 3000}
  db: {preset: postgres}
  cache: {preset: redis, secret_env: REDIS_PASS}
notifications: {critical: [ops], warning: [team]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveProject(cfg); err != nil {
		t.Fatal(err)
	}
	if err := d.SetEnv(map[string]string{"OBSERVA_TOKEN": "s3cret", cfg.EffectiveSecretEnv("db"): "postgresql://u:p@wulo-prod-db:5432/app", "REDIS_PASS": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveAttachRules([]AttachRule{{Container: "legacy.app_1", Project: "old", Env: "prod", Service: "legacy", Preset: "generic"}}); err != nil {
		t.Fatal(err)
	}
	changed, warns, err := d.Render(mustSettings(t, d))
	if err != nil || !changed || len(warns) != 0 {
		t.Fatalf("render: changed=%v warns=%v err=%v", changed, warns, err)
	}
	out := readFile(t, d.ConfigPath())
	for _, want := range []string{
		`url = "https://observa.example.com/ingest/prometheus/write"`,
		`endpoint = "https://observa.example.com/ingest/otlp"`,
		`username = "srv-a"`,
		`password = sys.env("OBSERVA_TOKEN")`,
		`ca_file = "/etc/alloy/ca.crt"`,
		`external_labels = { host = "srv-a" }`,
		`regex         = "/legacy\\.app_1"`,
		`regex         = "legacy\\.app_1"`,
		`prometheus.exporter.postgres "postgres_wulo_prod_db"`,
		`data_source_names = [sys.env("OBSERVA_SECRET_WULO_PROD_DB")]`,
		`redis_addr = "wulo-prod-cache:6379"`,
		`redis_password = sys.env("REDIS_PASS")`,
		`open_ports      = "3000"`,
		`namespace       = "wulo.prod"`,
		`set(attributes["observa.project"], "wulo") where attributes["service.name"] == "front"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config.alloy missing %s", want)
		}
	}
	if strings.Contains(out, "s3cret") {
		t.Error("the token must not be written in config.alloy")
	}
	if strings.Count(out, "{") != strings.Count(out, "}") {
		t.Error("unbalanced braces in config.alloy")
	}
	cmp := readFile(t, filepath.Join(d.Path, "docker-compose.yml"))
	if !strings.Contains(cmp, "pid: host") || !strings.Contains(cmp, "./config:/etc/alloy:ro") {
		t.Errorf("compose:\n%s", cmp)
	}
	prom := readFile(t, filepath.Join(d.Path, "textfile", "observa_wulo__prod.prom"))
	if !strings.Contains(prom, `observa_service_threshold{project="wulo",env="prod",service="api",threshold="http_latency_p95_seconds"} 0.75`) ||
		!strings.Contains(prom, `notify_critical="ops",notify_warning="team"`) {
		t.Errorf("threshold metrics:\n%s", prom)
	}
	st, _ := os.Stat(filepath.Join(d.Path, ".env"))
	if st.Mode().Perm() != 0o600 {
		t.Errorf(".env mode %v", st.Mode().Perm())
	}
}

func TestPostgresWithoutSecretIsSkipped(t *testing.T) {
	d := newAgent(t, &Settings{Name: "srv-a", Host: "srv-a", CentralURL: "https://c.example.com"})
	cfg, _ := project.Parse([]byte("version: 1\nproject: p\nenv: prod\nservices: {db: {preset: postgres}}\n"))
	d.SaveProject(cfg)
	_, warns, err := d.Render(mustSettings(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "OBSERVA_SECRET_P_PROD_DB") {
		t.Fatalf("warnings: %v", warns)
	}
	if out := readFile(t, d.ConfigPath()); strings.Contains(out, "prometheus.exporter.postgres") {
		t.Error("postgres exporter rendered without its secret")
	}
}

func TestDesktopComposeHasNoHostMounts(t *testing.T) {
	d := newAgent(t, &Settings{Name: "local", Host: "mac", CentralURL: "http://observa-caddy:3000", Desktop: true})
	if _, _, err := d.Render(mustSettings(t, d)); err != nil {
		t.Fatal(err)
	}
	cmp := readFile(t, filepath.Join(d.Path, "docker-compose.yml"))
	if strings.Contains(cmp, "/:/rootfs") || strings.Contains(cmp, "pid: host") {
		t.Errorf("desktop compose has host mounts:\n%s", cmp)
	}
	if out := readFile(t, d.ConfigPath()); strings.Contains(out, "/host/proc") || strings.Contains(out, "beyla") {
		t.Error("desktop config must not reference host mounts or eBPF")
	}
}

func TestAlloyString(t *testing.T) {
	if got := alloyString(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("alloyString = %s", got)
	}
}

func mustSettings(t *testing.T, d Dir) *Settings {
	t.Helper()
	s, err := d.Settings()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestEnvFileQuoting(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	if err := d.SetEnv(map[string]string{"A": "pa$word #1", "B": "x=y"}); err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, filepath.Join(d.Path, ".env"))
	if !strings.Contains(raw, "A='pa$word #1'") {
		t.Errorf(".env:\n%s", raw)
	}
	env, _ := d.Env()
	if env["A"] != "pa$word #1" || env["B"] != "x=y" {
		t.Errorf("round trip: %v", env)
	}
	if err := d.SetEnv(map[string]string{"C": "it's"}); err == nil {
		t.Error("apostrophe accepted")
	}
}
