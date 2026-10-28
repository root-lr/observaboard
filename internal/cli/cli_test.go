package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/internal/dockerx"
)

// run executes the CLI in dry-run mode and returns its output and exit code.
func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	stdout, stderr, stdin = &out, &out, strings.NewReader("")
	in = nil
	dockerx.Out = &out
	dockerx.DryRun = true
	t.Cleanup(func() { dockerx.DryRun = false })
	code := Run(args)
	return out.String(), code
}

func TestAllInOneWithExampleDryRun(t *testing.T) {
	dir := t.TempDir()
	out, code := run(t, "up", "--all-in-one", "--with-example", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, f := range []string{
		"central/central.yaml", "central/caddy/Caddyfile", "central/prometheus/rules/observa.yml",
		"agent/config/config.alloy", "agent/.env", "agent/projects/boutique__local.yaml",
		"agent/textfile/observa_boutique__local.prom",
		"example/observa.yaml", "example/docker-compose.observa.yml", "example/Dockerfile.observa",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	env, _ := os.ReadFile(filepath.Join(dir, "agent", ".env"))
	if !strings.Contains(string(env), "OBSERVA_SECRET_BOUTIQUE_LOCAL_DB='postgresql://shop:shop@boutique-local-db:5432/shop") {
		t.Errorf("agent .env:\n%s", env)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "agent", "config", "config.alloy"))
	if !strings.Contains(string(cfg), `url = "http://observa-caddy:3000/ingest/prometheus/write"`) || !strings.Contains(string(cfg), "prometheus.exporter.postgres") {
		t.Errorf("agent config does not target the local central or misses postgres")
	}
	// Re-running keeps the same token (idempotent).
	before, _ := os.ReadFile(filepath.Join(dir, "central", "tokens.yaml"))
	if out, code := run(t, "up", "--all-in-one", "--dir", dir); code != 0 {
		t.Fatalf("second run exit %d:\n%s", code, out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "central", "tokens.yaml"))
	if !bytes.Equal(before, after) {
		t.Error("token was recreated on a second run")
	}
	if out, code := run(t, "down", "--all-in-one", "--dir", dir); code != 0 {
		t.Fatalf("down exit %d:\n%s", code, out)
	}
}

func TestServerFlowDryRun(t *testing.T) {
	root := t.TempDir()
	cdir, adir := filepath.Join(root, "central"), filepath.Join(root, "agent")
	out, code := run(t, "central", "install", "--dir", cdir, "--domain", "observa.example.com", "--first-agent", "srv-a")
	if code != 0 {
		t.Fatalf("central install exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "observa agent install --central https://observa.example.com --token srv-a.") {
		t.Errorf("install output lacks the agent command:\n%s", out)
	}
	tok := ""
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "srv-a.") {
			tok = f
		}
	}
	out, code = run(t, "agent", "install", "--central", "https://observa.example.com", "--token", tok, "--dir", adir, "--host", "srv-a")
	if code != 0 {
		t.Fatalf("agent install exit %d:\n%s", code, out)
	}
	proj := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "examples", "django-shop"), proj)
	out, code = run(t, "init", "--dir", proj, "--project", "boutique", "--env", "prod", "--yes")
	if code != 0 {
		t.Fatalf("init exit %d:\n%s", code, out)
	}
	out, code = run(t, "apply", filepath.Join(proj, "observa.yaml"), "--agent-dir", adir)
	if code != 0 || !strings.Contains(out, "boutique/prod registered") {
		t.Fatalf("apply exit %d:\n%s", code, out)
	}
	out, code = run(t, "attach", "legacy_app", "--project", "old", "--preset", "generic", "--agent-dir", adir)
	if code != 0 {
		t.Fatalf("attach exit %d:\n%s", code, out)
	}
	out, code = run(t, "token", "revoke", "srv-a", "--dir", cdir)
	if code != 0 {
		t.Fatalf("revoke exit %d:\n%s", code, out)
	}
	caddy, _ := os.ReadFile(filepath.Join(cdir, "caddy", "Caddyfile"))
	if strings.Contains(string(caddy), "srv-a $2a$") {
		t.Error("revoked token still in the Caddyfile")
	}
	// Usage errors exit with code 2.
	if _, code := run(t, "agent", "install"); code != 2 {
		t.Errorf("missing flags exit code = %d, want 2", code)
	}
	if _, code := run(t, "nope"); code != 2 {
		t.Errorf("unknown command exit code = %d, want 2", code)
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentReinstallOptions(t *testing.T) {
	adir := t.TempDir()
	base := []string{"agent", "install", "--central", "https://c.example.com", "--token", "srv-a.s", "--dir", adir, "--host", "srv-a"}
	read := func() string {
		b, _ := os.ReadFile(filepath.Join(adir, "agent.yaml"))
		return string(b)
	}
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	os.WriteFile(caFile, []byte("-----BEGIN CERTIFICATE-----\n"), 0o644)
	if out, code := run(t, append(base, "--ebpf", "--ca-file", caFile)...); code != 0 {
		t.Fatalf("install: %s", out)
	}
	if !strings.Contains(read(), "ebpf: true") || !strings.Contains(read(), "ca_file: true") {
		t.Fatalf("first install: %s", read())
	}
	run(t, base...) // re-run without options: kept
	if !strings.Contains(read(), "ebpf: true") || !strings.Contains(read(), "ca_file: true") {
		t.Errorf("options lost on re-install: %s", read())
	}
	run(t, append(base, "--ebpf=false")...)
	if strings.Contains(read(), "ebpf: true") {
		t.Errorf("--ebpf=false ignored: %s", read())
	}
	other := append([]string{}, base...)
	other[3] = "https://other.example.com"
	run(t, other...)
	if strings.Contains(read(), "ca_file: true") {
		t.Errorf("TLS trust kept for a different central: %s", read())
	}
	// token from a file
	tf := filepath.Join(t.TempDir(), "tok")
	os.WriteFile(tf, []byte("srv-b.secret\n"), 0o600)
	if out, code := run(t, "agent", "install", "--central", "https://c.example.com", "--token-file", tf, "--dir", t.TempDir()); code != 0 {
		t.Errorf("--token-file: %s", out)
	}
}

func TestInitReadsPipedAnswers(t *testing.T) {
	proj := t.TempDir()
	copyDir(t, filepath.Join("..", "..", "examples", "django-shop"), proj)
	var out bytes.Buffer
	stdout, stderr = &out, &out
	// project, env, then one answer per service (db, redis, traffic, web, worker)
	stdin = strings.NewReader("shop\nstaging\n\n\nignore\n\n\n")
	in = nil
	dockerx.DryRun = true
	defer func() { dockerx.DryRun = false }()
	if code := Run([]string{"init", "--dir", proj}); code != 0 {
		t.Fatalf("init: %s", out.String())
	}
	data, _ := os.ReadFile(filepath.Join(proj, "observa.yaml"))
	s := string(data)
	if !strings.Contains(s, "project: shop") || !strings.Contains(s, "env: staging") || strings.Contains(s, "traffic:\n") || !strings.Contains(s, "- traffic") {
		t.Fatalf("answers not applied:\n%s", s)
	}
	// Re-running keeps the ignored service out.
	out.Reset()
	if code := Run([]string{"init", "--dir", proj, "--yes"}); code != 0 {
		t.Fatalf("re-init: %s", out.String())
	}
	data, _ = os.ReadFile(filepath.Join(proj, "observa.yaml"))
	if strings.Contains(string(data), "traffic:\n") {
		t.Errorf("ignored service came back:\n%s", data)
	}
}
