package central

import (
	"bytes"
	"fmt"

	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

type amRoute struct {
	Receiver       string    `yaml:"receiver"`
	Matchers       []string  `yaml:"matchers,omitempty"`
	GroupBy        []string  `yaml:"group_by,omitempty"`
	GroupWait      string    `yaml:"group_wait,omitempty"`
	GroupInterval  string    `yaml:"group_interval,omitempty"`
	RepeatInterval string    `yaml:"repeat_interval,omitempty"`
	Continue       bool      `yaml:"continue,omitempty"`
	Routes         []amRoute `yaml:"routes,omitempty"`
}

type amReceiver struct {
	Name     string           `yaml:"name"`
	Email    []map[string]any `yaml:"email_configs,omitempty"`
	Slack    []map[string]any `yaml:"slack_configs,omitempty"`
	Telegram []map[string]any `yaml:"telegram_configs,omitempty"`
	Webhook  []map[string]any `yaml:"webhook_configs,omitempty"`
}

type amConfig struct {
	Global       map[string]any   `yaml:"global,omitempty"`
	Route        amRoute          `yaml:"route"`
	InhibitRules []map[string]any `yaml:"inhibit_rules,omitempty"`
	Receivers    []amReceiver     `yaml:"receivers"`
}

const slackText = `{{ range .Alerts }}{{ if eq .Status "firing" }}:red_circle:{{ else }}:white_check_mark:{{ end }} {{ .Annotations.summary }}
{{ .Annotations.description }}
Runbook : {{ .Annotations.runbook_url }} — Dashboard : {{ .Annotations.dashboard_url }}
{{ end }}`

const telegramText = `{{ range .Alerts }}{{ if eq .Status "firing" }}🔴{{ else }}✅{{ end }} <b>{{ .Labels.alertname }}</b>
{{ .Annotations.summary }}
{{ .Annotations.description }}
<a href="{{ .Annotations.runbook_url }}">Runbook</a> · <a href="{{ .Annotations.dashboard_url }}">Dashboard</a>
{{ end }}`

func channelReceiver(name string, ch Channel) amReceiver {
	r := amReceiver{Name: name}
	addInto(&r, ch)
	return r
}

func addInto(r *amReceiver, ch Channel) {
	switch ch.Type {
	case "email":
		r.Email = append(r.Email, map[string]any{"to": ch.To, "send_resolved": true})
	case "slack":
		c := map[string]any{
			"api_url":       ch.WebhookURL,
			"send_resolved": true,
			"title":         `[{{ .Status | toUpper }}] {{ .CommonLabels.alertname }}`,
			"text":          slackText,
		}
		if ch.SlackChannel != "" {
			c["channel"] = ch.SlackChannel
		}
		r.Slack = append(r.Slack, c)
	case "telegram":
		r.Telegram = append(r.Telegram, map[string]any{
			"bot_token":     ch.BotToken,
			"chat_id":       ch.ChatID,
			"send_resolved": true,
			"parse_mode":    "HTML",
			"message":       telegramText,
		})
	case "webhook":
		r.Webhook = append(r.Webhook, map[string]any{"url": ch.URL, "send_resolved": true})
	}
}

// RenderAlertmanager builds alertmanager.yml from the notification settings.
// Routing: an alert goes to the channels named in its notify_critical or
// notify_warning label (set from observa.yaml), otherwise to the default channels.
func RenderAlertmanager(s *Settings) ([]byte, error) {
	n := s.Notifications
	cfg := amConfig{
		Route: amRoute{
			Receiver:       "default",
			GroupBy:        []string{"alertname", "project", "env", "service", "host"},
			GroupWait:      "30s",
			GroupInterval:  "5m",
			RepeatInterval: "4h",
		},
		InhibitRules: []map[string]any{
			{ // a critical alert hides warnings for the same service
				"source_matchers": []string{`severity="critical"`},
				"target_matchers": []string{`severity="warning"`},
				"equal":           []string{"project", "env", "service"},
			},
			{ // a silent agent explains why its services look down
				"source_matchers": []string{`alertname="ObservaAgentSilent"`},
				"target_matchers": []string{`alertname=~"ObservaServiceDown|ObservaContainerRestarting"`},
				"equal":           []string{"host"},
			},
		},
	}
	if n.SMTP.Smarthost != "" {
		g := map[string]any{"smtp_smarthost": n.SMTP.Smarthost, "smtp_from": n.SMTP.From}
		if n.SMTP.Username != "" {
			g["smtp_auth_username"] = n.SMTP.Username
			g["smtp_auth_password"] = n.SMTP.Password
		}
		if n.SMTP.RequireTLS != nil {
			g["smtp_require_tls"] = *n.SMTP.RequireTLS
		}
		cfg.Global = g
	}

	// Watchdog: heartbeat to an external monitor, or dropped.
	watchdog := amRoute{Receiver: "blackhole", Matchers: []string{`alertname="Watchdog"`}}
	if n.HeartbeatURL != "" {
		watchdog = amRoute{
			Receiver: "heartbeat", Matchers: []string{`alertname="Watchdog"`},
			GroupWait: "0s", GroupInterval: "1m", RepeatInterval: "1m",
		}
	}
	cfg.Route.Routes = append(cfg.Route.Routes, watchdog)

	names := sortedChannelNames(n.Channels)
	for _, name := range names {
		for _, sev := range []string{"critical", "warning"} {
			cfg.Route.Routes = append(cfg.Route.Routes, amRoute{
				Receiver: "channel-" + name,
				Matchers: []string{
					fmt.Sprintf(`severity=%q`, sev),
					fmt.Sprintf(`notify_%s=~"(.+,)?%s(,.+)?"`, sev, name),
				},
				Continue: true,
			})
		}
	}

	def := amReceiver{Name: "default"}
	for _, d := range n.Default {
		if ch, ok := n.Channels[d]; ok {
			addInto(&def, ch)
		}
	}
	cfg.Receivers = append(cfg.Receivers, def, amReceiver{Name: "blackhole"})
	if n.HeartbeatURL != "" {
		cfg.Receivers = append(cfg.Receivers, amReceiver{
			Name:    "heartbeat",
			Webhook: []map[string]any{{"url": n.HeartbeatURL, "send_resolved": false}},
		})
	}
	for _, name := range names {
		cfg.Receivers = append(cfg.Receivers, channelReceiver("channel-"+name, n.Channels[name]))
	}

	var b bytes.Buffer
	b.WriteString("# Alertmanager du central ObservaBoard — généré depuis central.yaml (observa central reload).\n")
	if len(n.Default) == 0 {
		b.WriteString("# ATTENTION : aucun canal par défaut (notifications.default) — les alertes sans canal ne sont envoyées nulle part.\n")
	}
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	enc.Close()
	return b.Bytes(), nil
}
