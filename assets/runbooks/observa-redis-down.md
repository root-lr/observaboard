# ObservaRedisDown — Redis unreachable

**Severity:** critical · **Fires when:** the agent's exporter has been unable to connect to Redis for 2 minutes (`redis_up` = 0).

## What the alert means

Redis is stopped, refusing connections, or the agent can no longer reach it (`observa` network, password).

## User impact

Depending on how Redis is used: cache disabled (slowness), lost sessions (logouts), Celery queue blocked (tasks not run).

## Dashboards to open

1. **Redis**: *Reachable*, *Memory*, *Service logs*.
2. **Celery worker** if Redis is the broker.

## Useful queries

```logql
{project="PROJECT", env="ENV", service="SERVICE"}
```

```bash
docker compose ps SERVICE
docker exec -it <redis-container> redis-cli ping
```

## Likely causes

- **Redis stopped**: crash, OOM, server reboot.
- **Memory**: Redis killed by the kernel.
- **Password** changed without updating the agent's variable.

## Actions

1. Restart Redis: `docker compose up -d SERVICE`.
2. Password: `observa agent env set <VARIABLE>=<password>` then `observa agent reload`.

## Verify recovery

- *Reachable* goes back to 1, the Celery queue drains.
