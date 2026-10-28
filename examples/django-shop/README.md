# Example shop

A small Django shop that serves as a training ground: PostgreSQL, Redis, a Celery worker, a traffic generator, and simulated failures on demand.

## Run

The easiest way, from the repository root:

```bash
bin/observa up --all-in-one --with-example
```

By hand, with an agent already installed on the machine:

```bash
observa init --project boutique --env local --yes
observa apply
observa agent env set OBSERVA_SECRET_BOUTIQUE_LOCAL_DB='postgresql://shop:shop@boutique-local-db:5432/shop?sslmode=disable'
docker compose -f compose.yaml -f docker-compose.observa.yml up -d --build
```

The shop listens on http://localhost:8000.

## Pages and API

| Route | Behavior |
| --- | --- |
| `/` | HTML list, deliberately N+1: a burst of SQL queries visible in traces |
| `/api/products/`, `/api/products/<id>/` | JSON catalog |
| `POST /api/orders/` | order, call to a simulated "payment provider", then a Celery task that sends the confirmation |
| `/health/` | probe, excluded from traces |

## Simulate failures

```bash
docker compose exec web python manage.py chaos <mode> [--minutes 10]
```

| Mode | Effect | Expected alert |
| --- | --- | --- |
| `errors` | 30% of calls to `/api/products/` return 500 | ObservaHighErrorRate (≈ 5 min) |
| `latency` | +1.5 s on product pages and orders | ObservaHighLatency (≈ 10 min, threshold 800 ms) |
| `dependency` | the external payment provider stops responding (3 s timeout, 502 response) | ObservaHighErrorRate, latency |
| `tasks` | Celery tasks fail | ObservaCeleryTaskFailures (≈ 10 min) |
| `status` | shows active failures | — |
| `reset` | stops all failures | alerts resolve |

For each alert, follow the *runbook* link in the notification or open `/runbooks/` in Grafana, and practice tracking down the cause in the dashboards, traces and logs.
