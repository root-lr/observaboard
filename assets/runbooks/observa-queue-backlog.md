# ObservaQueueBacklog — the Celery queue is growing

**Severity:** warning · **Fires when:** more messages than the threshold are waiting in the Redis `celery` list for 10 minutes.
**Threshold:** 100 by default, configurable with `alerts.queue_length` on the Redis service in `observa.yaml`.

## What the alert means

Tasks arrive faster than the workers process them. Only the default `celery` queue (database 0) is monitored in V1.

## User impact

Asynchronous work falls behind: an email goes out several minutes late, an export never arrives.

## Dashboards to open

1. **Celery worker**: *Queue*, *Active workers*, *p95 duration by type*.
2. **Redis**: memory (a growing queue uses memory).

## Useful queries

```promql
redis_key_size{project="PROJECT", env="ENV", key="celery"}
```

```promql
count((time() - observa:service:last_seen_timestamp{project="PROJECT", env="ENV", service="WORKER"}) < 120)
```

## Likely causes

- **Workers stopped** or failing (see the ObservaServiceDown alert on the worker).
- **Tasks became slow** (slow dependency): p95 duration rises.
- **Task spike** (bulk import, email campaign).
- **Concurrency too low** (`--concurrency`).

## Actions

1. Check that the workers are running: `docker compose ps`.
2. If tasks are slow, address the cause (see the traces).
3. Add workers temporarily: `docker compose up -d --scale worker=3`.
4. Move slow tasks to a dedicated queue so they don't block the others.

## Verify recovery

- The *Queue* graph goes back down toward 0.
