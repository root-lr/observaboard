package central

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/root-lr/observaboard/assets"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/thirdparty/bcrypt"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

func serverSettings() *Settings {
	s := Defaults(ModeServer)
	s.Domain = "observa.example.com"
	tlsOn := true
	s.Notifications = Notifications{
		SMTP: SMTP{Smarthost: "smtp.example.com:587", From: "alerts@example.com", Username: "u", Password: "p", RequireTLS: &tlsOn},
		Channels: map[string]Channel{
			"email-me":     {Type: "email", To: "me@example.com"},
			"slack-wulo":   {Type: "slack", WebhookURL: "https://hooks.slack.com/services/X", SlackChannel: "#alerts"},
			"telegram-ops": {Type: "telegram", BotToken: "123:ABC", ChatID: -100123},
			"hook":         {Type: "webhook", URL: "https://example.com/hook"},
		},
		Default:      []string{"email-me"},
		HeartbeatURL: "https://hc-ping.com/uuid",
	}
	return s
}

func TestSettingsValidation(t *testing.T) {
	if errs := serverSettings().Validate(); len(errs) > 0 {
		t.Fatalf("valid settings rejected: %v", errs)
	}
	bad := serverSettings()
	bad.Domain = ""
	bad.Retention.Logs = "100h"
	bad.Notifications.Default = []string{"nope"}
	bad.Notifications.Channels["x"] = Channel{Type: "sms"}
	errs := strings.Join(bad.Validate(), "\n")
	for _, want := range []string{"domain (recommended) or ip", "multiple of 24", `channel "nope" is not defined`, `unknown type "sms"`} {
		if !strings.Contains(errs, want) {
			t.Errorf("missing error %q in:\n%s", want, errs)
		}
	}
	local := Defaults(ModeLocal)
	if errs := local.Validate(); len(errs) > 0 {
		t.Fatalf("local defaults rejected: %v", errs)
	}
	if local.BaseURL() != "http://localhost:3000" || local.SiteAddress() != ":3000" {
		t.Errorf("local URL %s / site %s", local.BaseURL(), local.SiteAddress())
	}
	ip := Defaults(ModeServer)
	ip.IP = "203.0.113.7"
	if ip.BaseURL() != "https://203.0.113.7" || ip.SiteAddress() != "203.0.113.7" {
		t.Errorf("ip URL %s / site %s", ip.BaseURL(), ip.SiteAddress())
	}
}

func renderAll(t *testing.T, s *Settings, tokens []Token) map[string][]byte {
	t.Helper()
	files, _, err := Files(s, tokens)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRenderedFilesAreValid(t *testing.T) {
	for _, s := range []*Settings{serverSettings(), Defaults(ModeLocal)} {
		files := renderAll(t, s, []Token{{Name: "srv-a", Hash: "$2a$04$abcdefghijklmnopqrstuuJ6bW3iQ7Gm1Zk6e2c8Qw0r1s2t3u4v5"}})
		for path, data := range files {
			switch {
			case strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".yaml"):
				var v any
				if err := yaml.Unmarshal(data, &v); err != nil {
					t.Errorf("%s (%s): invalid YAML: %v", path, s.Mode, err)
				}
			case strings.HasSuffix(path, ".json"):
				var v map[string]any
				if err := json.Unmarshal(data, &v); err != nil {
					t.Errorf("%s: invalid JSON: %v", path, err)
				}
			}
			if strings.Contains(string(data), "<no value>") {
				t.Errorf("%s: template left <no value>", path)
			}
		}
		caddy := string(files["caddy/Caddyfile"])
		if !strings.Contains(caddy, "srv-a $2a$04$") || !strings.Contains(caddy, s.SiteAddress()+" {") {
			t.Errorf("Caddyfile (%s):\n%s", s.Mode, caddy)
		}
		cmp := string(files["docker-compose.yml"])
		for _, p := range s.Ports() {
			if !strings.Contains(cmp, `"`+p+`"`) {
				t.Errorf("compose missing port %s", p)
			}
		}
	}
}

func TestCaddyWithoutTokensRefusesIngest(t *testing.T) {
	files := renderAll(t, serverSettings(), nil)
	if c := string(files["caddy/Caddyfile"]); strings.Contains(c, "basic_auth") || !regexp.MustCompile(`respond "[^"]+" 401`).MatchString(c) {
		t.Fatalf("Caddyfile without tokens:\n%s", c)
	}
}

func TestAlertmanagerRouting(t *testing.T) {
	data, err := RenderAlertmanager(serverSettings())
	if err != nil {
		t.Fatal(err)
	}
	var cfg amConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Global["smtp_smarthost"] != "smtp.example.com:587" || cfg.Global["smtp_require_tls"] != true {
		t.Errorf("global: %v", cfg.Global)
	}
	routes := map[string][]string{}
	for _, r := range cfg.Route.Routes {
		routes[r.Receiver] = append(routes[r.Receiver], strings.Join(r.Matchers, " "))
		if strings.HasPrefix(r.Receiver, "channel-") && !r.Continue {
			t.Errorf("channel route %s must continue", r.Receiver)
		}
	}
	if len(routes["channel-telegram-ops"]) != 2 || !strings.Contains(routes["channel-telegram-ops"][0], `notify_critical=~"(.+,)?telegram-ops(,.+)?"`) {
		t.Errorf("telegram routes: %v", routes["channel-telegram-ops"])
	}
	if _, ok := routes["heartbeat"]; !ok {
		t.Errorf("heartbeat route missing: %v", routes)
	}
	recv := map[string]amReceiver{}
	for _, r := range cfg.Receivers {
		recv[r.Name] = r
	}
	if len(recv["default"].Email) != 1 || len(recv["channel-telegram-ops"].Telegram) != 1 || len(recv["channel-hook"].Webhook) != 1 {
		t.Errorf("receivers: %+v", recv)
	}
	// The routing regex must match exact channel names inside the comma list.
	re := regexp.MustCompile(`^(?:(.+,)?ops(,.+)?)$`)
	for in, want := range map[string]bool{"ops": true, "a,ops": true, "ops,b": true, "a,ops,b": true, "devops": false, "ops-2": false} {
		if re.MatchString(in) != want {
			t.Errorf("routing regex on %q = %v", in, !want)
		}
	}
}

func TestTokens(t *testing.T) {
	d := Dir{Path: t.TempDir()}
	tok, err := d.CreateToken("srv-a")
	if err != nil {
		t.Fatal(err)
	}
	name, secret, err := SplitToken(tok)
	if err != nil || name != "srv-a" {
		t.Fatalf("split: %v %s", err, name)
	}
	tokens, _ := d.Tokens()
	if len(tokens) != 1 || bcrypt.CompareHashAndPassword([]byte(tokens[0].Hash), []byte(secret)) != nil {
		t.Fatalf("stored hash does not match the secret")
	}
	if _, err := d.CreateToken("srv-a"); err == nil {
		t.Error("duplicate token accepted")
	}
	if _, err := d.CreateToken("Bad Name"); err == nil {
		t.Error("invalid name accepted")
	}
	if err := d.RevokeToken("srv-a"); err != nil {
		t.Fatal(err)
	}
	if err := d.RevokeToken("srv-a"); err == nil {
		t.Error("revoking twice should fail")
	}
	for _, bad := range []string{"", "nodot", ".secret", "name."} {
		if _, _, err := SplitToken(bad); err == nil {
			t.Errorf("SplitToken(%q) accepted", bad)
		}
	}
}

// Every alert has a runbook, every runbook belongs to an alert, presets only
// reference existing alerts, and every threshold used by the rules is defined.
func TestRulesRunbooksPresetsAreConsistent(t *testing.T) {
	rules, err := RenderRules("https://x")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert       string            `yaml:"alert"`
				Record      string            `yaml:"record"`
				Expr        string            `yaml:"expr"`
				Labels      map[string]string `yaml:"labels"`
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(rules, &doc); err != nil {
		t.Fatal(err)
	}
	alerts := map[string]bool{}
	records := map[string]bool{}
	runbooksUsed := map[string]bool{}
	var exprs []string
	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			exprs = append(exprs, r.Expr)
			if r.Record != "" {
				records[r.Record] = true
				continue
			}
			alerts[r.Alert] = true
			if r.Alert == "Watchdog" {
				continue
			}
			if r.Labels["severity"] != "critical" && r.Labels["severity"] != "warning" {
				t.Errorf("%s: severity %q", r.Alert, r.Labels["severity"])
			}
			u := r.Annotations["runbook_url"]
			if !strings.HasPrefix(u, "https://x/runbooks/") {
				t.Errorf("%s: runbook_url %q", r.Alert, u)
			}
			runbooksUsed[strings.TrimPrefix(u, "https://x/runbooks/")] = true
			if r.Annotations["summary"] == "" || !strings.HasPrefix(r.Annotations["dashboard_url"], "https://x/d/") {
				t.Errorf("%s: summary/dashboard_url missing", r.Alert)
			}
		}
	}
	files, _ := fs.ReadDir(assets.FS, "runbooks")
	have := map[string]bool{}
	for _, f := range files {
		have[strings.TrimSuffix(f.Name(), ".md")] = true
	}
	for rb := range runbooksUsed {
		if !have[rb] {
			t.Errorf("runbook %s referenced but missing", rb)
		}
	}
	for rb := range have {
		if !runbooksUsed[rb] {
			t.Errorf("runbook %s exists but no alert uses it", rb)
		}
	}
	for _, p := range presets.All() {
		for _, a := range p.Alerts {
			if !alerts[a] {
				t.Errorf("preset %s lists unknown alert %s", p.Name, a)
			}
		}
	}
	// Every observa:* series used in an expression is recorded somewhere.
	used := regexp.MustCompile(`observa:[a-z0-9_:]+`)
	for _, e := range exprs {
		for _, m := range used.FindAllString(e, -1) {
			if !records[m] {
				t.Errorf("expression uses unrecorded series %s", m)
			}
		}
	}
	var missing []string
	for _, p := range presets.AllThresholdParams() {
		if !records["observa:threshold:"+p.Metric] {
			missing = append(missing, p.Metric)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("thresholds without fallback rule: %v", missing)
	}
}

func TestAddressEdgeCases(t *testing.T) {
	v6 := Defaults(ModeServer)
	v6.IP = "2001:db8::1"
	if v6.BaseURL() != "https://[2001:db8::1]" || v6.SiteAddress() != "[2001:db8::1]" {
		t.Errorf("ipv6: %s %s", v6.BaseURL(), v6.SiteAddress())
	}
	l := Defaults(ModeLocal)
	l.Listen = "localhost:3000"
	if p := l.Ports(); p[0] != "127.0.0.1:3000:3000" {
		t.Errorf("localhost listen: %v", p)
	}
	bad := Defaults(ModeServer)
	bad.Domain = "x.example.com"
	bad.Email = "a b\n}"
	if errs := bad.Validate(); len(errs) == 0 {
		t.Error("invalid email accepted")
	}
}
