# Boutique d'exemple

Une petite boutique Django qui sert de terrain d'entraînement : PostgreSQL, Redis, un worker Celery, un générateur de trafic, et des pannes simulées à la demande.

## Lancer

Le plus simple, depuis la racine du dépôt :

```bash
bin/observa up --all-in-one --with-example
```

À la main, avec un agent déjà installé sur la machine :

```bash
observa init --project boutique --env local --yes
observa apply
observa agent env set OBSERVA_SECRET_BOUTIQUE_LOCAL_DB='postgresql://shop:shop@boutique-local-db:5432/shop?sslmode=disable'
docker compose -f compose.yaml -f docker-compose.observa.yml up -d --build
```

La boutique répond sur http://localhost:8000.

## Pages et API

| Route | Comportement |
| --- | --- |
| `/` | liste HTML, volontairement en N+1 : une rafale de requêtes SQL visible dans les traces |
| `/api/products/`, `/api/products/<id>/` | catalogue JSON |
| `POST /api/orders/` | commande, appel à un « prestataire de paiement » simulé, puis tâche Celery d'envoi de confirmation |
| `/health/` | sonde, exclue des traces |

## Simuler des pannes

```bash
docker compose exec web python manage.py chaos <mode> [--minutes 10]
```

| Mode | Effet | Alerte attendue |
| --- | --- | --- |
| `errors` | 30 % des appels à `/api/products/` renvoient 500 | ObservaHighErrorRate (≈ 5 min) |
| `latency` | +1,5 s sur les pages produits et les commandes | ObservaHighLatency (≈ 10 min, seuil 800 ms) |
| `dependency` | le paiement externe ne répond plus (timeout 3 s, réponse 502) | ObservaHighErrorRate, latence |
| `tasks` | les tâches Celery échouent | ObservaCeleryTaskFailures (≈ 10 min) |
| `status` | affiche les pannes en cours | — |
| `reset` | arrête toutes les pannes | résolution des alertes |

Pour chaque alerte, suivez le lien *runbook* de la notification ou ouvrez `/runbooks/` dans Grafana, et entraînez-vous à retrouver la cause dans les dashboards, les traces et les logs.
