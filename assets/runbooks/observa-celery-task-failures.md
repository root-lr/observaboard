# ObservaCeleryTaskFailures — too many failed Celery tasks

**Severity:** warning · **Fires when:** the share of failed tasks stays above the threshold for 10 minutes, with at least one task every 8 minutes.
**Threshold:** 10% by default, configurable with `alerts.task_failure_pct` on the Celery service in `observa.yaml`.

## What the alert means

Tasks run by the worker raise exceptions. The figure comes from traces: each task run is a *consumer* span, marked as an error if the task failed.

## User impact

Asynchronous work not done: emails not sent, payments not confirmed, missing exports, depending on the affected tasks.

## Dashboards to open

1. **Celery worker**: *Failures / s by type* to see which task fails.
2. **Traces → Failed tasks**: the exception is recorded in the trace.
3. **Service logs** of the worker.

## Useful queries

```promql
sum by (span_name) (rate(traces_spanmetrics_calls_total{service="SERVICE", span_kind="SPAN_KIND_CONSUMER", status_code="STATUS_CODE_ERROR"}[10m]))
```

```traceql
{resource.service.name="SERVICE" && kind=consumer && status=error}
```

## Likely causes

- **Bug in a task** after a deployment.
- **Dependency unavailable**: external API, database, file storage.
- **Unexpected data** passed to the task.
- **Timeout** (`soft_time_limit`, `time_limit`) exceeded.

## Actions

1. Identify the task and the exception in a trace.
2. Fix it, then re-run the lost tasks if they are not retried automatically.
3. For transient errors (network), add `autoretry_for` with an increasing backoff.

## Verify recovery

- *Failed tasks* drops back below the threshold.
- The queue (Redis or Celery dashboard) is not growing.
