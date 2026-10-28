// Package central manages the central ObservaBoard server: its settings
// (central.yaml), agent tokens, and the rendering of every component's
// configuration (Caddy, Prometheus, Alertmanager, Loki, Tempo, Grafana).
package central

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/root-lr/observaboard/internal/fsutil"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

// DefaultDir is where the central server lives.
const DefaultDir = "/opt/observa/central"

// Modes.
const (
	ModeServer = "server" // public HTTPS (domain or IP)
	ModeLocal  = "local"  // all-in-one on a workstation, plain HTTP on 127.0.0.1
)

// Settings is central.yaml.
type Settings struct {
	Version int    `yaml:"version"`
	Mode    string `yaml:"mode"`
	// Domain: DNS name pointing to this server (automatic Let's Encrypt certificate).
	Domain string `yaml:"domain,omitempty"`
	// IP: public IP when there is no domain (certificate from Caddy's internal CA).
	IP string `yaml:"ip,omitempty"`
	// Email: contact address for Let's Encrypt.
	Email string `yaml:"email,omitempty"`
	// Listen: local mode only, address:port published on the workstation.
	Listen string `yaml:"listen,omitempty"`

	AdminUser string    `yaml:"admin_user"`
	Retention Retention `yaml:"retention"`

	Notifications Notifications `yaml:"notifications"`
}

// Retention per signal.
type Retention struct {
	Metrics string `yaml:"metrics"` // Prometheus duration, e.g. 15d
	Logs    string `yaml:"logs"`    // Loki duration, multiple of 24h, e.g. 168h
	Traces  string `yaml:"traces"`  // Tempo duration, e.g. 72h
}

// Notifications: where alerts go.
type Notifications struct {
	SMTP SMTP `yaml:"smtp,omitempty"`
	// Channels are referenced by name from observa.yaml (notifications.critical / warning).
	Channels map[string]Channel `yaml:"channels,omitempty"`
	// Default receives alerts of services that name no channel, and host alerts.
	Default []string `yaml:"default,omitempty"`
	// HeartbeatURL receives the always-firing Watchdog alert every minute
	// (healthchecks.io, Uptime Kuma push...). Its silence means the central is down.
	HeartbeatURL string `yaml:"heartbeat_url,omitempty"`
}

// SMTP server used by email channels.
type SMTP struct {
	Smarthost  string `yaml:"smarthost,omitempty"` // host:port
	From       string `yaml:"from,omitempty"`
	Username   string `yaml:"username,omitempty"`
	Password   string `yaml:"password,omitempty"`
	RequireTLS *bool  `yaml:"require_tls,omitempty"`
}

// Channel is one notification destination.
type Channel struct {
	Type string `yaml:"type"` // email | slack | telegram | webhook
	// email
	To string `yaml:"to,omitempty"`
	// slack
	WebhookURL   string `yaml:"webhook_url,omitempty"`
	SlackChannel string `yaml:"channel,omitempty"`
	// telegram
	BotToken string `yaml:"bot_token,omitempty"`
	ChatID   int64  `yaml:"chat_id,omitempty"`
	// webhook
	URL string `yaml:"url,omitempty"`
}

var (
	promDurationRE = regexp.MustCompile(`^[0-9]+(ms|s|m|h|d|w|y)$`)
	goHoursRE      = regexp.MustCompile(`^[0-9]+h$`)
	domainRE       = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	emailRE        = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
)

// Defaults returns settings with sensible defaults for a mode.
func Defaults(mode string) *Settings {
	s := &Settings{
		Version:   1,
		Mode:      mode,
		AdminUser: "admin",
		Retention: Retention{Metrics: "15d", Logs: "168h", Traces: "72h"},
	}
	if mode == ModeLocal {
		s.Listen = "127.0.0.1:3000"
	}
	return s
}

// Validate checks central.yaml.
func (s *Settings) Validate() []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	switch s.Mode {
	case ModeServer:
		switch {
		case s.Domain == "" && s.IP == "":
			add("mode server: set domain (recommended) or ip")
		case s.Domain != "" && s.IP != "":
			add("domain and ip are mutually exclusive")
		case s.Domain != "" && !domainRE.MatchString(strings.ToLower(s.Domain)):
			add("invalid domain %q", s.Domain)
		case s.IP != "" && net.ParseIP(s.IP) == nil:
			add("invalid ip %q", s.IP)
		}
	case ModeLocal:
		host, port, err := net.SplitHostPort(s.Listen)
		if err != nil || port == "" {
			add("invalid listen %q (e.g. 127.0.0.1:3000)", s.Listen)
		} else if host != "" && net.ParseIP(host) == nil && host != "localhost" {
			add("listen: the host must be an IP (e.g. 127.0.0.1)")
		}
	default:
		add("unknown mode %q (server or local)", s.Mode)
	}
	if s.Email != "" && !emailRE.MatchString(s.Email) {
		add("invalid email %q", s.Email)
	}
	if !project.SlugRE.MatchString(s.AdminUser) {
		add("invalid admin_user %q", s.AdminUser)
	}
	if !promDurationRE.MatchString(s.Retention.Metrics) {
		add("invalid retention.metrics %q (e.g. 15d)", s.Retention.Metrics)
	}
	if !goHoursRE.MatchString(s.Retention.Logs) {
		add("invalid retention.logs %q: in hours, a multiple of 24 (e.g. 168h)", s.Retention.Logs)
	} else {
		var h int
		fmt.Sscanf(s.Retention.Logs, "%dh", &h)
		if h%24 != 0 || h < 24 {
			add("retention.logs must be a multiple of 24h (e.g. 168h)")
		}
	}
	if !goHoursRE.MatchString(s.Retention.Traces) {
		add("invalid retention.traces %q: in hours (e.g. 72h)", s.Retention.Traces)
	}
	n := s.Notifications
	needSMTP := false
	for _, name := range sortedChannelNames(n.Channels) {
		ch := n.Channels[name]
		where := "notifications.channels." + name
		if !project.SlugRE.MatchString(name) {
			add("%s: invalid name (lowercase letters, digits, hyphens)", where)
		}
		switch ch.Type {
		case "email":
			if ch.To == "" {
				add("%s: to is required", where)
			}
			needSMTP = true
		case "slack":
			if !strings.HasPrefix(ch.WebhookURL, "https://") {
				add("%s: an https webhook_url is required", where)
			}
		case "telegram":
			if ch.BotToken == "" || ch.ChatID == 0 {
				add("%s: bot_token and chat_id (integer) are required", where)
			}
		case "webhook":
			if !strings.HasPrefix(ch.URL, "http://") && !strings.HasPrefix(ch.URL, "https://") {
				add("%s: an http(s) url is required", where)
			}
		default:
			add("%s: unknown type %q (email, slack, telegram, webhook)", where, ch.Type)
		}
	}
	if needSMTP && (n.SMTP.Smarthost == "" || n.SMTP.From == "") {
		add("notifications.smtp: smarthost and from are required for email channels")
	}
	for _, d := range n.Default {
		if _, ok := n.Channels[d]; !ok {
			add("notifications.default: channel %q is not defined", d)
		}
	}
	if n.HeartbeatURL != "" && !strings.HasPrefix(n.HeartbeatURL, "http") {
		add("notifications.heartbeat_url must be an http(s) URL")
	}
	return errs
}

func sortedChannelNames(m map[string]Channel) []string {
	var k []string
	for name := range m {
		k = append(k, name)
	}
	sort.Strings(k)
	return k
}

// BaseURL is the public URL of the central server.
func (s *Settings) BaseURL() string {
	switch {
	case s.Mode == ModeLocal:
		host, port, _ := net.SplitHostPort(s.Listen)
		if host == "" || host == "0.0.0.0" || host == "127.0.0.1" {
			host = "localhost"
		}
		return "http://" + host + ":" + port
	case s.Domain != "":
		return "https://" + s.Domain
	default:
		return "https://" + hostForURL(s.IP)
	}
}

// hostForURL puts IPv6 addresses in brackets.
func hostForURL(ip string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]"
	}
	return ip
}

// SiteAddress is the Caddy site address.
func (s *Settings) SiteAddress() string {
	switch {
	case s.Mode == ModeLocal:
		return ":3000"
	case s.Domain != "":
		return s.Domain
	default:
		return hostForURL(s.IP)
	}
}

// Ports published by Caddy.
func (s *Settings) Ports() []string {
	if s.Mode == ModeLocal {
		host, port, _ := net.SplitHostPort(s.Listen)
		if host == "localhost" || host == "" {
			host = "127.0.0.1" // Compose only accepts IP addresses
		}
		return []string{net.JoinHostPort(host, port) + ":3000"}
	}
	return []string{"80:80", "443:443", "443:443/udp"}
}

// Dir is a central directory on disk.
type Dir struct{ Path string }

const settingsFile = "central.yaml"

// Exists reports whether a central is installed in the directory.
func (d Dir) Exists() bool { return fsutil.Exists(filepath.Join(d.Path, settingsFile)) }

// Settings loads central.yaml.
func (d Dir) Settings() (*Settings, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, settingsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no central in %s (observa central install, or --dir)", d.Path)
		}
		return nil, err
	}
	s := Defaults(ModeServer)
	s.Listen = ""
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", settingsFile, err)
	}
	if errs := s.Validate(); len(errs) > 0 {
		return s, fmt.Errorf("invalid %s:\n  - %s", settingsFile, strings.Join(errs, "\n  - "))
	}
	return s, nil
}

const settingsHeader = `# ObservaBoard central settings.
# After editing: observa central reload
#
# Notification channels (examples):
#   notifications:
#     smtp: { smarthost: "smtp.example.com:587", from: "alerts@example.com", username: "...", password: "..." }
#     channels:
#       email-me:     { type: email, to: "me@example.com" }
#       slack-team:   { type: slack, webhook_url: "https://hooks.slack.com/services/...", channel: "#alerts" }
#       telegram-ops: { type: telegram, bot_token: "123456:ABC...", chat_id: -1001234567890 }
#       my-webhook:   { type: webhook, url: "https://example.com/hook" }
#     default: [email-me]           # alerts with no channel set in observa.yaml
#     heartbeat_url: "https://hc-ping.com/<uuid>"   # optional, receives the Watchdog every minute
`

// SaveSettings writes central.yaml (mode 0600: it may contain secrets).
func (d Dir) SaveSettings(s *Settings) error {
	data, err := marshal2(s)
	if err != nil {
		return err
	}
	_, err = fsutil.WriteFile(filepath.Join(d.Path, settingsFile), append([]byte(settingsHeader), data...), 0o600)
	return err
}

// marshal2 encodes YAML with a 2-space indent.
func marshal2(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
