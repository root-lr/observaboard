# ObservaServiceDown — a service is no longer running

**Severity:** critical · **Fires when:** the agent has not seen the service's container for more than 2 minutes.

## What the alert means

The container carrying this service's `observa.*` labels no longer exists or is stopped on the reported server (`host`). The alert stays active for 24 hours if the service was intentionally removed.

## User impact

Everything this service provides is unavailable: API, website, background tasks or database, depending on the service.

## Dashboards to open

1. **Service · container**: *Last seen* and *Restarts*; the last logs before the stop are in *Service logs*.
2. **Servers**: check that the server is not saturated (memory, disk).

## Useful queries

Last log lines before the stop:

```logql
{project="PROJECT", env="ENV", service="SERVICE"}
```

How long the service has been unseen:

```promql
time() - observa:service:last_seen_timestamp{project="PROJECT", env="ENV", service="SERVICE"}
```

On the server:

```bash
docker ps -a --filter label=observa.service=SERVICE
docker logs --tail 100 <container>
docker inspect <container> --format '{{.State.ExitCode}} {{.State.OOMKilled}} {{.State.Error}}'
```

## Likely causes

- **Crash at startup**: configuration error, migration, missing dependency. Non-zero exit code.
- **Killed for lack of memory**: `OOMKilled` is `true`.
- **Manual stop** or interrupted deployment (`docker compose down` without `up`).
- **Server rebooted** with no `restart:` policy on the service.
- **Service intentionally removed**.

## Actions

1. Restart the service: `docker compose up -d SERVICE`, then follow its logs.
2. OOM: raise the memory limit or fix the leak.
3. Add `restart: unless-stopped` to the service if it is not already there.
4. Service intentionally removed: remove it from `observa.yaml`, run `observa apply` again, and silence the alert for 24 h (Grafana › Alerting › Silences, Alertmanager source).

## Verify recovery

- *Last seen* goes back to a few seconds.
- The alert resolves after a minute.
