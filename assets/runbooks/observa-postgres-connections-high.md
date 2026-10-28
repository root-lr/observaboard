# ObservaPostgresConnectionsHigh — PostgreSQL connections almost exhausted

**Severity:** warning · **Fires when:** open connections stay above the threshold of `max_connections` for 10 minutes.
**Threshold:** 80% by default, configurable with `alerts.connections_pct` in `observa.yaml`.

## What the alert means

The number of connections is approaching the limit. Beyond it, PostgreSQL refuses new connections and applications return errors.

## User impact

None for now; outright errors if the limit is reached.

## Dashboards to open

1. **PostgreSQL**: *Connections by state*. Many `idle` connections = connections kept open without work; many `idle in transaction` = transactions never finished.
2. **Longest active transaction**.

## Useful queries

```promql
sum by (state) (pg_stat_activity_count{project="PROJECT", env="ENV", service="SERVICE"})
```

In the database, who is using the connections:

```sql
SELECT application_name, usename, state, count(*)
FROM pg_stat_activity GROUP BY 1, 2, 3 ORDER BY 4 DESC;
```

## Likely causes

- **Too many application workers**, each with its own connection pool.
- **Persistent connections** (`CONN_MAX_AGE` in Django) multiplied by the number of processes.
- **Connection leak** or transactions never closed.

## Actions

1. Work out the need: processes × connections per process, across all applications.
2. Reduce `CONN_MAX_AGE` or the number of workers, or put PgBouncer in front of the database.
3. Terminate stuck transactions (`pg_terminate_backend`) after analysis.
4. As a last resort, increase `max_connections` (costs memory).

## Verify recovery

- The share of used connections drops back below the threshold.
