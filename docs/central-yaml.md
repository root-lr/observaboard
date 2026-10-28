# Reference: central.yaml

`central.yaml` holds the central server's settings, including the notification channels' secrets (file mode 0600). After every change:

```bash
sudo observa central reload
```

`reload` regenerates the configurations and hot-reloads what it can: Caddy, Prometheus and Alertmanager. Loki, Tempo and Grafana restart if their configuration changed.

```yaml
version: 1
mode: server                 # server | local (all-in-one)
domain: observa.example.com  # or ip: 203.0.113.7 (internal certificate)
email: you@example.com       # Let's Encrypt contact (optional)
admin_user: admin
retention:
  metrics: 15d               # Prometheus
  logs: 168h                 # Loki, multiple of 24h
  traces: 72h                # Tempo
notifications:
  smtp:
    smarthost: smtp.example.com:587
    from: alerts@example.com
    username: alerts@example.com
    password: "…"
    require_tls: true
  channels:
    email-me:       { type: email, to: me@example.com }
    slack-wulo:     { type: slack, webhook_url: "https://hooks.slack.com/services/…", channel: "#alerts" }
    telegram-ops:   { type: telegram, bot_token: "123456:ABC…", chat_id: -1001234567890 }
    my-webhook:     { type: webhook, url: "https://example.com/hook" }
  default: [email-me]
  heartbeat_url: https://hc-ping.com/<uuid>
```

## Alert routing

| Alert | Recipients |
| --- | --- |
| Alert on a service whose project declares `notifications.critical` / `warning` | the channels listed for its severity |
| Alert on a service with no declared channel, server alert (disk, memory, silent agent), central alert | `default` |
| `Watchdog` (always firing) | `heartbeat_url` every minute, otherwise ignored |

Inhibition rules:
- a critical alert inhibits warnings for the same service;
- a silent agent inhibits the "service down" alerts of its server.

Check routing without waiting for a real failure:

```bash
sudo observa alert test                                         # default channels
sudo observa alert test --notify-critical telegram-ops          # a project's channel
```

## Silences

To mute an alert temporarily (maintenance, service intentionally removed), go to Grafana, *Alerting › Silences*, then pick the **Alertmanager** source. It is provisioned automatically.

## External heartbeat

If Prometheus or Alertmanager go down, no alert is sent anymore. Create a check with an external service such as healthchecks.io or an Uptime Kuma push monitor, with a one-minute period and a 5-minute grace time, then put its URL in `heartbeat_url`. Its silence signals that the central has failed.

## Telegram

1. Create a bot with @BotFather and get its `bot_token`.
2. Add the bot to the target group.
3. Get the group's `chat_id` (a negative number) from `https://api.telegram.org/bot<token>/getUpdates`.
4. Write `chat_id` as a number, without quotes.
