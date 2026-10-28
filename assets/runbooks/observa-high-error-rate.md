# ObservaHighErrorRate — too many 5xx responses

**Severity:** critical · **Fires when:** the share of 5xx responses stays above the threshold for 5 minutes, with at least 3 requests per minute.
**Threshold:** 5% by default, configurable per service with `alerts.error_rate_pct` in `observa.yaml`.

## What the alert means

The service answers an abnormal share of requests with a server error (500, 502, 503, 504…). 4xx errors (invalid requests, 404) do not count.

## User impact

Pages or API calls fail. Depending on the affected routes, this ranges from a minor feature to a completely unusable service.

## Dashboards to open

1. **HTTP service** (the `dashboard_url` link in the notification): *Traffic* and *Routes* rows to see which routes fail and since when.
2. **Traces → Failed requests** in the same dashboard: open a trace to see the failing step (SQL query, external call, exception).
3. **Service logs** at the bottom of the dashboard: look for the stack trace (traceback).

## Useful queries

Routes returning 5xx (Prometheus):

```promql
topk(10, sum by (http_route, http_response_status_code) (
  rate(http_server_request_duration_seconds_count{project="PROJECT", env="ENV", service="SERVICE", http_response_status_code=~"5.."}[5m])
))
```

Exceptions in the logs (Loki):

```logql
{project="PROJECT", env="ENV", service="SERVICE"} |~ "(?i)(traceback|exception|error)"
```

Failed traces (Tempo, TraceQL):

```traceql
{resource.service.name="SERVICE" && kind=server && status=error}
```

## Likely causes

- **Recent deployment**: a regression in the code. Compare the start time with the last deployment.
- **Dependency unavailable**: database, Redis, external API. Traces show a client span failing or timing out.
- **Saturation**: all gunicorn/uvicorn workers busy, SQL connection pool exhausted. Latency rises at the same time.
- **Configuration**: missing environment variable, expired secret, unapplied migration.

## Actions

1. If a deployment just happened and errors started right after: **roll back to the previous version**, then investigate calmly.
2. If a dependency is at fault: check its state (PostgreSQL or Redis dashboards, external provider status page).
3. If it is saturation: increase the number of workers or replicas, and check the connection pool.
4. Fix the cause, then add a test that reproduces the error.

## Verify recovery

- The *Share of 5xx responses and threshold* graph drops back below the threshold and stays there for 10 minutes.
- The alert switches to *resolved* (a resolution notification is sent).
- No new traces in *Failed requests*.
