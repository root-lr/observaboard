# Référence : observa.yaml

`observa.yaml` décrit un projet. Il vit dans le dépôt du projet, est créé par `observa init` et appliqué sur chaque serveur par `observa apply`. Relancer `observa init` conserve vos modifications : les services déjà décrits ne sont pas touchés.

```yaml
version: 1
project: wulo          # minuscules, chiffres, tirets (1 à 40 caractères)
env: prod              # prod, staging, preprod…

services:
  api:
    preset: django
    runtime: python        # python | node — détecté par observa init
    instrumentation: otel  # otel | ebpf | none — défaut selon le preset
    port: 8000             # port interne
    healthcheck: /health/  # exclu des traces
    alerts:
      latency_p95_ms: 800  # défaut 1000
      error_rate_pct: 2    # défaut 5

  worker:
    preset: celery
    alerts:
      task_failure_pct: 20

  db:
    preset: postgres
    # Variable du .env de l'agent contenant la chaîne de connexion de l'exporter.
    # Défaut : OBSERVA_SECRET_<PROJET>_<ENV>_<SERVICE>
    secret_env: OBSERVA_SECRET_WULO_PROD_DB

  cache:
    preset: redis
    alerts:
      queue_length: 500    # file Celery

  legacy:
    preset: generic
    container: legacy_app_1   # conteneur lancé avec docker run

ignored: [traffic]           # services du compose volontairement non surveillés

notifications:               # canaux définis dans central.yaml
  critical: [telegram-ops, email-moi]
  warning: [slack-wulo]
```

Répondre « ignorer » à `observa init` pour un service l'ajoute à `ignored` : il n'est plus proposé aux exécutions suivantes. Les noms de services suivent ceux du compose (`celery_worker` est accepté) ; l'alias réseau et l'image dérivée en utilisent une forme compatible DNS (`celery-worker`).

## Champs d'un service

| Champ | Défaut | Description |
| --- | --- | --- |
| `preset` | requis | `django`, `fastapi`, `celery`, `postgres`, `redis`, `generic` (`observa preset list`) |
| `runtime` | selon le preset | `python` ou `node` : active l'instrumentation sans code |
| `instrumentation` | `otel` si un runtime est connu, sinon `none` | `otel` (SDK OpenTelemetry, niveau 2), `ebpf` (agent `--ebpf` requis, port requis), `none` |
| `port` | selon le preset | port d'écoute interne ; sert aux exporters et à eBPF |
| `healthcheck` | — | chemin HTTP exclu des traces |
| `container` | `<projet>-<env>-<service>` | nom du conteneur pour les services lancés avec `docker run` |
| `secret_env` | `OBSERVA_SECRET_…` | variable de l'agent contenant le DSN PostgreSQL ou le mot de passe Redis |
| `alerts` | défauts du preset | seuils surchargés (voir ci-dessous) |

## Seuils (`alerts`)

| Preset | Clé | Défaut | Unité |
| --- | --- | --- | --- |
| django, fastapi, generic | `latency_p95_ms` | 1000 | millisecondes |
| django, fastapi, generic | `error_rate_pct` | 5 | % de réponses 5xx |
| celery | `task_failure_pct` | 10 | % de tâches en échec |
| postgres | `connections_pct` | 80 | % de `max_connections` |
| redis | `memory_pct` | 90 | % de `maxmemory` |
| redis | `queue_length` | 100 | messages dans la file `celery` |

Une clé inconnue ou une valeur hors limites est refusée par `observa validate`, qui est utilisable en CI.

## Secrets des bases de données

L'exporter PostgreSQL a besoin d'un utilisateur en lecture. Créez-le dans la base, puis enregistrez la chaîne de connexion **sur le serveur**. Elle n'est jamais écrite dans le dépôt.

```sql
CREATE USER observa WITH PASSWORD '…';
GRANT pg_monitor TO observa;
```

```bash
observa agent env set OBSERVA_SECRET_WULO_PROD_DB='postgresql://observa:…@wulo-prod-db:5432/wulo?sslmode=disable'
```

L'hôte est l'alias du service sur le réseau `observa` (`<projet>-<env>-<service>`).

## Notifications

`critical` et `warning` listent des canaux **définis dans `central.yaml`**. Sans `notifications`, les alertes du projet vont aux canaux par défaut du central.
