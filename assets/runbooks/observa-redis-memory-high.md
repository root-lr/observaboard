# ObservaRedisMemoryHigh — Redis close to maxmemory

**Severity:** warning · **Fires when:** memory used stays above the threshold of `maxmemory` for 10 minutes. Without `maxmemory` configured, the alert never fires.
**Threshold:** 90% by default, configurable with `alerts.memory_pct` in `observa.yaml`.

## What the alert means

Redis is approaching its limit. Depending on `maxmemory-policy`, it will evict keys (`allkeys-lru`…) or refuse writes (`noeviction`, the default).

## User impact

With `noeviction`: write errors, Celery tasks rejected. With eviction: less effective cache, lost sessions.

## Dashboards to open

1. **Redis**: *Memory*, *Keys per database*, *Evicted keys / s*, *Celery queue*.

## Useful queries

```promql
redis_memory_used_bytes{project="PROJECT", env="ENV", service="SERVICE"} / redis_memory_max_bytes{project="PROJECT", env="ENV", service="SERVICE"}
```

```bash
docker exec -it <redis-container> redis-cli --bigkeys
docker exec -it <redis-container> redis-cli info memory
```

## Likely causes

- **Growing Celery queue** (workers stopped).
- **Keys without expiry** piling up.
- **Big keys** (lists, hashes).

## Actions

1. Celery queue: see the ObservaQueueBacklog runbook.
2. Add expiry times (TTL) to cache keys.
3. Pick a suitable policy: `allkeys-lru` for a pure cache, `noeviction` for a queue.
4. Increase `maxmemory` if the server allows it.

## Verify recovery

- Memory drops back below the threshold and stays stable.
