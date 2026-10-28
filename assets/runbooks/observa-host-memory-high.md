# ObservaHostMemoryHigh — server memory almost exhausted

**Severity:** warning · **Fires when:** more than 90% of memory has been in use for 10 minutes.

## What the alert means

Little memory is left. The kernel will soon kill processes (OOM killer), often the largest one: the database or the application.

## User impact

Slowness (swap), then abrupt service restarts.

## Dashboards to open

1. **Servers**: *Memory used* and *Memory of the 10 hungriest containers*.

## Useful queries

```promql
topk(5, sum by (container) (container_memory_working_set_bytes{host="HOST", container!=""}))
```

```bash
docker stats --no-stream
free -h
```

## Likely causes

- **Memory leak** in an application: its graph rises and never comes down.
- **Too many workers** (gunicorn, Celery) for the server size.
- Database **cache** sized too large.

## Actions

1. Identify the growing container.
2. Leak: restart it until the fix lands; for gunicorn, `--max-requests` recycles workers.
3. Add memory limits to containers (`mem_limit` / `deploy.resources.limits.memory`).
4. Reduce the number of workers, or add memory to the server.

## Verify recovery

- Memory used drops back below 90% and stays stable.
