# Reference: observa.yaml

`observa.yaml` describes a project. It lives in the project's repository, is created by `observa init` and applied on each server by `observa apply`. Running `observa init` again keeps your changes: services already described are left untouched.

```yaml
version: 1
project: wulo          # lowercase letters, digits, hyphens (1 to 40 characters)
env: prod              # prod, staging, preprod…

services:
  api:
    preset: django
    runtime: python        # python | node — detected by observa init
    instrumentation: otel  # otel | ebpf | none — default depends on the preset
    port: 8000             # internal port
    healthcheck: /health/  # excluded from traces
    alerts:
      latency_p95_ms: 800  # default 1000
      error_rate_pct: 2    # default 5

  worker:
    preset: celery
    alerts:
      task_failure_pct: 20

  db:
    preset: postgres
    # Variable in the agent's .env holding the exporter's connection string.
    # Default: OBSERVA_SECRET_<PROJECT>_<ENV>_<SERVICE>
    secret_env: OBSERVA_SECRET_WULO_PROD_DB

  cache:
    preset: redis
    alerts:
      queue_length: 500    # Celery queue

  legacy:
    preset: generic
    container: legacy_app_1   # container started with docker run

ignored: [traffic]           # compose services intentionally not monitored

notifications:               # channels defined in central.yaml
  critical: [telegram-ops, email-me]
  warning: [slack-wulo]
```

Answering "ignore" to `observa init` for a service adds it to `ignored`: it is no longer offered on later runs. Service names follow the compose file (`celery_worker` is accepted); the network alias and the derived image use a DNS-compatible form (`celery-worker`).

## Service fields

| Field | Default | Description |
| --- | --- | --- |
| `preset` | required | `django`, `fastapi`, `celery`, `postgres`, `redis`, `generic` (`observa preset list`) |
| `runtime` | depends on the preset | `python` or `node`: enables zero-code instrumentation |
| `instrumentation` | `otel` if a runtime is known, otherwise `none` | `otel` (OpenTelemetry SDK, level 2), `ebpf` (requires agent `--ebpf` and a port), `none` |
| `port` | depends on the preset | internal listening port; used by exporters and eBPF |
| `healthcheck` | — | HTTP path excluded from traces |
| `container` | `<project>-<env>-<service>` | container name for services started with `docker run` |
| `secret_env` | `OBSERVA_SECRET_…` | agent variable holding the PostgreSQL DSN or the Redis password |
| `alerts` | preset defaults | overridden thresholds (see below) |

## Thresholds (`alerts`)

| Preset | Key | Default | Unit |
| --- | --- | --- | --- |
| django, fastapi, generic | `latency_p95_ms` | 1000 | milliseconds |
| django, fastapi, generic | `error_rate_pct` | 5 | % of 5xx responses |
| celery | `task_failure_pct` | 10 | % of failed tasks |
| postgres | `connections_pct` | 80 | % of `max_connections` |
| redis | `memory_pct` | 90 | % of `maxmemory` |
| redis | `queue_length` | 100 | messages in the `celery` queue |

An unknown key or an out-of-range value is rejected by `observa validate`, which can be used in CI.

## Database secrets

The PostgreSQL exporter needs a read-only user. Create it in the database, then store the connection string **on the server**. It is never written to the repository.

```sql
CREATE USER observa WITH PASSWORD '…';
GRANT pg_monitor TO observa;
```

```bash
observa agent env set OBSERVA_SECRET_WULO_PROD_DB='postgresql://observa:…@wulo-prod-db:5432/wulo?sslmode=disable'
```

The host is the service's alias on the `observa` network (`<project>-<env>-<service>`).

## Notifications

`critical` and `warning` list channels **defined in `central.yaml`**. Without `notifications`, the project's alerts go to the central's default channels.
