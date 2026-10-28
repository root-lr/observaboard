# ObservaHighLatency — p95 latency too high

**Severity:** warning · **Fires when:** p95 latency stays above the threshold for 10 minutes, with at least 3 requests per minute.
**Threshold:** 1000 ms by default, configurable per service with `alerts.latency_p95_ms` in `observa.yaml`.

## What the alert means

5% of requests take longer than the threshold. The service works, but some users wait too long.

## User impact

Slow pages, client-side or proxy timeouts, and eventually 504 errors if the slowdown gets worse.

## Dashboards to open

1. **HTTP service**: *Latency* and *Slowest routes (p95)* panels, to see whether all routes are affected or just one.
2. **Dependencies** row: *Time spent in the database* and *Outgoing calls* show where the time goes.
3. **Traces → Slow requests (> 1 s)**: open a trace; the longest bar is the cause.
4. **Service · container**: container CPU and memory, to rule out saturation.

## Useful queries

Slowest routes:

```promql
topk(5, histogram_quantile(0.95, sum by (http_route, le) (
  rate(http_server_request_duration_seconds_bucket{project="PROJECT", env="ENV", service="SERVICE"}[5m])
)))
```

Time spent in the database per second (1 = one core fully busy waiting for the database):

```promql
sum(rate(traces_spanmetrics_latency_sum{service="SERVICE", span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))
```

Slow traces:

```traceql
{resource.service.name="SERVICE" && kind=server && duration > 1s}
```

## Likely causes

- **Slow or too many SQL queries** (N+1 in a Django view): many SQL spans in a single trace.
- **Slow external call**: an HTTP client span takes up most of the trace.
- **Lack of resources**: container CPU maxed out, memory close to the limit, busy server.
- **Database locks**: see *Longest active transaction* in the PostgreSQL dashboard.
- **Traffic spike**: the *Requests / s* graph rose at the same time.

## Actions

1. Identify the route and the slow step in a trace.
2. N+1: add `select_related` / `prefetch_related` (Django) or batch the queries.
3. Slow query: add the missing index (`EXPLAIN ANALYZE`).
4. External call: add a timeout or a cache, or move the call to an asynchronous task.
5. Saturation: increase resources or the number of workers.

## Verify recovery

- The p95 graph drops back below the *p95 alert threshold* line.
- New traces for the affected route are shorter.
