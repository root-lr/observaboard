package project

import (
	"strings"
	"testing"
)

const valid = `
version: 1
project: wulo
env: prod
services:
  api:
    preset: django
    port: 8000
    healthcheck: /health/
    alerts:
      latency_p95_ms: 800
      error_rate_pct: 2
  worker:
    preset: celery
  db:
    preset: postgres
  cache:
    preset: redis
    alerts:
      queue_length: 500
notifications:
  critical: [telegram-ops, email-moi]
  warning: [slack-wulo]
`

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(valid))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.Services["api"].EffectiveInstrumentation(); got != "otel" {
		t.Errorf("django instrumentation = %q, want otel", got)
	}
	if got := c.Services["db"].EffectiveInstrumentation(); got != "none" {
		t.Errorf("postgres instrumentation = %q, want none", got)
	}
	if got := c.Services["worker"].EffectiveRuntime(); got != "python" {
		t.Errorf("celery runtime = %q, want python", got)
	}
	if got := c.EffectiveSecretEnv("db"); got != "OBSERVA_SECRET_WULO_PROD_DB" {
		t.Errorf("secret env = %q", got)
	}
	if got := Alias("wulo", "prod", "api"); got != "wulo-prod-api" {
		t.Errorf("alias = %q", got)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"unknown key":        {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django}}\nfoo: 1\n", "field foo not found"},
		"bad version":        {"version: 2\nproject: a\nenv: prod\nservices: {api: {preset: django}}\n", "version doit valoir 1"},
		"bad project":        {"version: 1\nproject: Mon_Projet\nenv: prod\nservices: {api: {preset: django}}\n", "project"},
		"no services":        {"version: 1\nproject: a\nenv: prod\nservices: {}\n", "au moins un service"},
		"unknown preset":     {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: rails}}\n", "preset \"rails\" inconnu"},
		"unknown alert":      {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django, alerts: {cpu: 3}}}\n", "alerts.cpu inconnu"},
		"alert out of range": {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django, alerts: {error_rate_pct: 300}}}\n", "hors limites"},
		"otel on postgres":   {"version: 1\nproject: a\nenv: prod\nservices: {db: {preset: postgres, instrumentation: otel, runtime: python}}\n", "sans objet"},
		"otel wrong runtime": {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django, runtime: node, instrumentation: otel}}\n", "attend le runtime python"},
		"bad healthcheck":    {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django, healthcheck: health}}\n", "doit commencer par /"},
		"bad channel":        {"version: 1\nproject: a\nenv: prod\nservices: {api: {preset: django}}\nnotifications: {critical: [Ops Team]}\n", "canal"},
		"ebpf without port":  {"version: 1\nproject: a\nenv: prod\nservices: {x: {preset: generic, instrumentation: ebpf}}\n", "port requis"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	c, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Parse(data)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, data)
	}
	if c2.Services["api"].Alerts["latency_p95_ms"] != 800 || len(c2.Notifications.Critical) != 2 {
		t.Fatalf("round trip lost data:\n%s", data)
	}
}

func TestComposeStyleServiceNames(t *testing.T) {
	c, err := Parse([]byte("version: 1\nproject: p\nenv: prod\nservices: {celery_worker: {preset: celery}, Web.App: {preset: django}}\n"))
	if err != nil {
		t.Fatalf("compose-style names rejected: %v", err)
	}
	if Alias("p", "prod", "celery_worker") != "p-prod-celery-worker" || Alias("p", "prod", "Web.App") != "p-prod-web-app" {
		t.Errorf("aliases: %s %s", Alias("p", "prod", "celery_worker"), Alias("p", "prod", "Web.App"))
	}
	if got := c.EffectiveSecretEnv("Web.App"); got != "OBSERVA_SECRET_P_PROD_WEB_APP" {
		t.Errorf("secret env %s", got)
	}
}
