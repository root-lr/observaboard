# ObservaContainerRestarting — restart loop

**Severity:** warning · **Fires when:** the same container restarts at least 3 times in 15 minutes.

## What the alert means

Docker restarts the container (`restart:` policy) because its main process exits. The service is available only intermittently.

## User impact

Occasional errors, dropped requests, interrupted tasks, lost sessions.

## Dashboards to open

1. **Service · container**: *Restarts* and *Memory* panels (memory climbing to the limit before each restart = OOM), and *Service logs*.

## Useful queries

```promql
changes(container_start_time_seconds{project="PROJECT", env="ENV", service="SERVICE"}[1h])
```

```logql
{project="PROJECT", env="ENV", service="SERVICE"} |~ "(?i)(error|exception|killed|fatal)"
```

```bash
docker inspect <container> --format '{{.RestartCount}} {{.State.OOMKilled}} {{.State.ExitCode}}'
```

## Likely causes

- **Memory**: the container hits its limit and is killed (`OOMKilled`).
- **Healthcheck** failing and triggering a restart.
- **Dependency missing at startup**: the app exits if the database or Redis is not answering yet.
- Intermittent **configuration error**.

## Actions

1. Read the last log lines before each restart.
2. OOM: raise the limit or reduce consumption (number of workers, cache sizes).
3. Dependency: add a wait or retries at startup, or `depends_on` with `condition: service_healthy`.

## Verify recovery

- No restart for 15 minutes.
- Memory stays stable below the limit.
