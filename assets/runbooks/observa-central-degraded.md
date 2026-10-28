# ObservaCentralDegraded — a central component is not responding

**Severity:** critical · **Fires when:** a central component (Prometheus, Alertmanager, Loki, Tempo, Grafana) has not answered Prometheus's health check for 2 minutes.

## What the alert means

Part of the observability platform has failed: depending on the component, logs, traces or dashboards are no longer available. If Prometheus or Alertmanager go down, this alert cannot be sent: that is the job of the external heartbeat (`notifications.heartbeat_url` in `central.yaml`).

## User impact

None on the monitored applications. You lose visibility, and data can be lost if the failure lasts longer than the agents' buffer (about 2 hours).

## Dashboards to open

Grafana itself may be at fault: work from the central server.

## Useful queries

```bash
cd /opt/observa/central
docker compose ps
docker compose logs --tail 100 <component>
observa doctor --central-dir /opt/observa/central
df -h
```

## Likely causes

- **Disk full** on the central server (components stop).
- **Not enough memory**: the central needs about 3 to 4 GB.
- **Invalid configuration** after editing `central.yaml`.
- **Failed upgrade** of a component.

## Actions

1. Disk: reduce retention or grow the disk.
2. Restart the component: `docker compose up -d <component>`.
3. Configuration: `observa central reload` shows the errors; fix `central.yaml`.

## Verify recovery

- `docker compose ps` shows all components `healthy`.
- Agents resend their buffered data; check the Overview.
