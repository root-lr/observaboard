# ObservaPostgresDown — PostgreSQL unreachable

**Severity:** critical · **Fires when:** the agent's exporter has been unable to connect to the database for 2 minutes (`pg_up` = 0).

## What the alert means

Either PostgreSQL is stopped or refusing connections, or the agent can no longer reach it (`observa` network, credentials).

## User impact

If the database is really down, every feature that depends on it fails: 5xx errors on the APIs.

## Dashboards to open

1. **PostgreSQL**: *Reachable* and *Service logs*.
2. **HTTP service** of the applications using the database: if they show no errors, the problem is the agent's connection, not the database.

## Useful queries

```logql
{project="PROJECT", env="ENV", service="SERVICE"} |~ "(?i)(fatal|panic|error|could not)"
```

On the server:

```bash
docker compose ps SERVICE
docker logs --tail 50 observa-agent 2>&1 | grep -i postgres
docker exec -it <postgres-container> pg_isready
```

## Likely causes

- **Database stopped** or restarting.
- **Disk full**: PostgreSQL stops when it can no longer write.
- **Too many connections**: `FATAL: sorry, too many clients already`.
- **Invalid agent credentials**: password changed, wrong variable in the agent's `.env`.
- **Container no longer on the `observa` network** (override not applied).

## Actions

1. Database stopped: `docker compose up -d SERVICE` and read its logs.
2. Disk full: free up space (see the ObservaHostDiskFull runbook).
3. Credentials: `observa agent env set <VARIABLE>='postgresql://...'` then `observa agent reload`.

## Verify recovery

- *Reachable* goes back to 1 and the applications no longer return errors.
