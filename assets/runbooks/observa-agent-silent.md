# ObservaAgentSilent — a server no longer sends data

**Severity:** critical · **Fires when:** no metric has been received from the agent for more than 5 minutes, although it was sending some in the last 24 hours.

## What the alert means

The central no longer receives anything from server `host`. ObservaServiceDown alerts for this server are inhibited while this one is active, since they follow from it.

## User impact

If the server is down: all its services are unavailable. If only the agent is at fault: no user impact, but you are blind on this server.

## Dashboards to open

1. **Servers**: the last received value shows when the cut happened.
2. **Overview**: are other servers affected? If all of them are, look at the central (runbook ObservaCentralDegraded).

## Useful queries

On the affected server:

```bash
observa agent status
docker logs --tail 100 observa-agent
curl -sS -u <agent>:<token> https://<central>/ingest/ping
```

## Likely causes

- **Server down** or unreachable (failure, reboot, network outage).
- **Agent stopped**: `observa-agent` missing from `docker ps`.
- **Token revoked**: pushes get a 401 (visible in the agent logs).
- **Disk full** on the server.
- **Central certificate** not trusted (IP mode without `--ca-file`).

## Actions

1. Check that the server responds (SSH, hosting provider console).
2. Restart the agent: `cd /opt/observa/agent && docker compose up -d`.
3. Token: create a new one on the central (`observa token create <agent>`), then reinstall the agent with it.
4. Server intentionally removed: revoke its token and silence the alert for 24 h (Grafana › Alerting › Silences, Alertmanager source).

## Verify recovery

- The server's metrics reappear in the Servers dashboard; the alert resolves within a minute.
- The agent automatically catches up on data buffered during a short outage.
