# Architecture

ObservaBoard separates **the central**, which stores, displays and alerts, from **the agents**, one per server, which collect and push. The `observa` CLI generates and maintains all the configuration on both sides.

## Components

| Side | Component | Version | Role |
| --- | --- | --- | --- |
| Central | Caddy | 2.11 | Only exposed port: HTTPS (Let's Encrypt or internal CA), agent authentication, runbooks, proxy to Grafana |
| Central | Prometheus | 3.15 | Receives metrics (remote write), evaluates alert rules |
| Central | Loki | 3.7 | Stores logs |
| Central | Tempo | 3.1 (single binary) | Stores traces, **computes HTTP and Celery metrics from traces** |
| Central | Alertmanager | 0.34 | Groups alerts and routes them to channels |
| Central | Grafana | 13.3 | Dashboards, exploration, logs ↔ traces correlation |
| Agent | Grafana Alloy | 1.21 | Docker discovery, logs, cAdvisor, host metrics, OTLP receiver, eBPF (Beyla), PostgreSQL and Redis exporters |

Versions are pinned in `internal/version/version.go`.

## Data flow

```text
Application (OpenTelemetry SDK, level 2) ──OTLP/HTTP──▶ agent :4318 (Docker network `observa`)
Containers (Docker socket) ──────────────────────────────▶ agent: logs + cAdvisor
Host (/proc, /sys, textfile) ────────────────────────────▶ agent: host metrics, thresholds, routing
PostgreSQL / Redis (`observa` network) ──────────────────▶ agent: exporters

agent ──HTTPS + basic auth (agent, token)──▶ Caddy ─┬─ /ingest/prometheus/write ─▶ Prometheus (remote write)
                                                    ├─ /ingest/loki/push ────────▶ Loki
                                                    └─ /ingest/otlp/v1/traces ───▶ Tempo
Tempo (metrics generator: span metrics, service graph) ──remote write──▶ Prometheus
Prometheus (rules) ──▶ Alertmanager ──▶ email · Slack · Telegram · webhook · heartbeat
```

The agent buffers on disk (WAL): a network outage of a few hours does not lose metrics.

## Labels, the backbone

Every series, log line and trace carries the same labels:

| Label | Source |
| --- | --- |
| `project`, `env`, `service`, `preset` | Docker `observa.*` labels (set by the override or `observa attach`), `observa.*` resource attributes (OpenTelemetry) |
| `host` | external label added by the agent (`--host`, server name by default) |
| `container` | container name |

Dashboards filter on these labels. Alert rules aggregate by `project, env, service`. Notification routing uses the `notify_critical` and `notify_warning` labels, joined onto the alerts.

## Why HTTP metrics come from traces

OpenTelemetry SDKs can export HTTP metrics, but those metrics become wrong with multi-process servers such as gunicorn or uvicorn with workers: each worker exports the same series, and their counters get mixed up. ObservaBoard therefore disables SDK metrics (`OTEL_METRICS_EXPORTER=none`) and has Tempo compute **span metrics** from server spans:

- `traces_spanmetrics_calls_total{project, env, service, span_kind, status_code, http_request_method, http_route, http_response_status_code}`
- `traces_spanmetrics_latency_bucket{…, le}`

This is correct whatever the number of workers. It is the same for every language and for eBPF traces. And it yields dependencies (SQL, outgoing calls) with no configuration. The trade-off: traces must be sampled at 100%, the default setting. `project` and `env` are set by Tempo's `dimension_mappings` from the `observa.project` and `observa.env` attributes.

## Thresholds and routing without central configuration

`observa apply` writes a file on the agent that is read by the *textfile collector*:

```text
observa_service_info{project="wulo",env="prod",service="api",preset="django",notify_critical="telegram-ops",notify_warning="slack"} 1
observa_service_threshold{project="wulo",env="prod",service="api",threshold="http_latency_p95_seconds"} 0.8
```

The central's rules combine these series with default values:

```promql
observa:threshold:http_latency_p95_seconds =
  max by (project, env, service) (observa_service_threshold{threshold="http_latency_p95_seconds"})
  or on (project, env, service) (observa:service:present * 0 + 1)
```

Each alert is multiplied by `observa:service:routing`, which equals 1, with `group_left (notify_critical, notify_warning)`. The routing labels are thus added without changing the value. Alertmanager then routes with a regular expression on these labels (`notify_critical=~"(.+,)?telegram-ops(,.+)?"`). The channels themselves, with their secrets, are defined only on the central.

Result: a project changes its thresholds and recipients in its own repository, without touching the central or redeploying rules.

## The `observa` Docker network

The agent and the monitored services share an external bridge network, `observa`. The override adds each service to this network with a unique alias (`<project>-<env>-<service>`), in addition to its original network. This lets:

- applications send their traces to `http://observa-agent:4318`;
- the agent's exporters reach `wulo-prod-db:5432` without publishing a port.

## Generated files

| Central (`/opt/observa/central`) | Agent (`/opt/observa/agent`) |
| --- | --- |
| `central.yaml` (settings, channels, 0600) | `agent.yaml` (settings) |
| `tokens.yaml` (bcrypt hashes, 0600) | `.env` (token, exporter secrets, 0600) |
| `.env` (initial Grafana admin password) | `config.alloy` (generated) |
| `docker-compose.yml`, `caddy/`, `prometheus/`, `alertmanager/`, `loki/`, `tempo/`, `grafana/`, `runbooks/` (generated) | `docker-compose.yml`, `config/config.alloy` (+ `config/ca.crt`), `projects/*.yaml`, `attach.yaml`, `textfile/*.prom` |

Containers mount **directories**, never single files. Files are rewritten atomically, i.e. by replacement. A file mounted on its own would stay on the old version, and hot reloads (Caddy, Prometheus, Alertmanager, Alloy) would not see the changes.

The central directory has mode 0700 because it holds the secrets. Containers only see the mounted subdirectories.

Generated files must not be edited by hand. Edit `central.yaml` or `observa.yaml`, then run `observa central reload` or `observa apply`.
