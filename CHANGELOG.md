# Changelog

## 0.1.0 — V1 (2026-10-28)

First release: the `observa` CLI and the full stack.

- **Central:** Caddy (automatic HTTPS, agent authentication by token), Prometheus, Loki, Tempo (span metrics), Alertmanager (routing by project and severity, inhibitions, heartbeat), Grafana (provisioned data sources and dashboards).
- **Agent:** Grafana Alloy, with Docker discovery by labels, logs, cAdvisor, host metrics, OTLP receiver, optional eBPF, PostgreSQL and Redis exporters. The agent pushes to the central, and a disk buffer protects against outages.
- **Projects:**
  - `observa init`: stack detection, `observa.yaml`, Compose override, derived image with OpenTelemetry for Python and Node.js;
  - `observa apply`: thresholds and routing published by the agent;
  - `observa attach` for existing containers;
  - `docker run` mode.
- **Presets:** django, fastapi, celery, postgres, redis, generic. 7 dashboards, 14 alerts with their runbooks.
- **All-in-one:** `observa up --all-in-one --with-example` starts the example Django shop, a traffic generator and simulated failures.
- **Tools:** `observa doctor`, `observa alert test`, `--dry-run`.
- **CI:** Go tests, promtool rule tests, config validation with the official binaries, end-to-end test, release binaries.
