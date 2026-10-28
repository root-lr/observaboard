# Presets et langages

Un preset regroupe ce qu'ObservaBoard sait d'une technologie :
- comment la reconnaître ;
- comment l'instrumenter ;
- quel dashboard l'affiche ;
- quelles alertes la concernent, et avec quels seuils par défaut.

`observa preset list` et `observa preset show <nom>` affichent cette information.

## Presets V1

| Preset | Dashboard | Instrumentation | Données principales |
| --- | --- | --- | --- |
| `django` | Service HTTP | OpenTelemetry Python sans code | requêtes par route, erreurs, latences p50/p95/p99, temps SQL, appels sortants, traces lentes et en erreur |
| `fastapi` | Service HTTP | OpenTelemetry Python sans code | idem |
| `celery` | Worker Celery | OpenTelemetry Python (tâches = spans *consumer*) | tâches/s par type, échecs, durée p95, file Redis, workers actifs |
| `postgres` | PostgreSQL | exporter de l'agent | connexions par état, transactions, cache, lignes, verrous, taille |
| `redis` | Redis | exporter de l'agent | mémoire, clients, commandes, taux de succès, évictions, file Celery |
| `generic` | Service · conteneur | aucune, ou Node.js sans code, ou eBPF | disponibilité, CPU, mémoire, réseau, redémarrages, logs, HTTP si instrumenté |

Tous les dashboards proposent les logs du service. Une ligne contenant `trace_id=…` ouvre la trace correspondante.

## Matrice de support

| Technologie | Niveau 1 (logs, ressources) | Traces et métriques HTTP | Preset dédié |
| --- | --- | --- | --- |
| Python : Django, FastAPI, Flask, Odoo… | V1 | V1 (zero-code) | Django, FastAPI, Celery en V1 |
| Node.js : NestJS, Express, Next.js serveur | V1 | V1 (zero-code, Node 22.15+ pour la dernière version de l'instrumentation) | V2 |
| Java, PHP, .NET, Ruby | V1 | eBPF en V1, zero-code en V2 | V2 |
| Go, Rust, autres | V1 | eBPF en V1, SDK manuel | après V2 |
| PostgreSQL, Redis/Valkey | V1 | — | V1 |
| MySQL, MongoDB, RabbitMQ, Nginx | V1 | — | V2 |

## Comment fonctionne l'instrumentation sans code

**Python.** L'image dérivée installe `opentelemetry-distro`, `opentelemetry-exporter-otlp` et, via `opentelemetry-bootstrap -a install`, les instrumentations des bibliothèques présentes : Django, psycopg, requests, redis, celery… La commande est préfixée par `opentelemetry-instrument`. Les sous-processus Python lancés par un `sh -c` héritent de l'instrumentation.

**Node.js.** L'image dérivée installe `@opentelemetry/auto-instrumentations-node`. `NODE_OPTIONS` reçoit `--require @opentelemetry/auto-instrumentations-node/register` ; une valeur existante est conservée.

**Variables posées par l'override.** Les plus importantes :

| Variable | Valeur |
| --- | --- |
| `OTEL_SERVICE_NAME` | nom du service |
| `OTEL_RESOURCE_ATTRIBUTES` | `observa.project`, `observa.env`, `observa.service`, `observa.preset` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://observa-agent:4318` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (la distro Python utilise gRPC par défaut) |
| `OTEL_SEMCONV_STABILITY_OPT_IN` | `http` (conventions HTTP stables : `http.route`, `http.response.status_code`) |
| `OTEL_METRICS_EXPORTER` | `none` (voir [architecture](architecture.md#pourquoi-les-métriques-http-viennent-des-traces)) |
| `OTEL_PYTHON_LOG_CORRELATION` | `true` |
| `OTEL_PYTHON_EXCLUDED_URLS` | le `healthcheck` du service |

## Corréler logs et traces en Python

`OTEL_PYTHON_LOG_CORRELATION=true` ajoute `otelTraceID` et `otelSpanID` à chaque enregistrement de log. Le format par défaut ne s'applique pas si votre application configure elle-même ses logs, ce qui est le cas de Django avec `LOGGING`. Ajoutez alors ces champs à votre formatter :

```python
"format": "%(asctime)s %(levelname)s %(name)s trace_id=%(otelTraceID)s %(message)s"
```

L'exemple `examples/django-shop/shop/settings.py` montre une configuration complète, avec un filtre qui évite l'erreur quand OpenTelemetry n'est pas actif.

## Gunicorn et serveurs multi-processus

Les traces sont exportées correctement par chaque worker. Le SDK relance ses threads d'export après le `fork`, ce qui a été vérifié avec 3 workers. Évitez l'option `--preload`, qui initialise l'application avant le fork.

## Écrire un preset

Un preset touche quatre endroits :

1. `internal/presets/presets.go` : nom, dashboard, alertes, seuils (`Param`), exporter éventuel.
2. `internal/detect/detect.go` : règles de reconnaissance (image, dépendance, commande).
3. `internal/dashboards/dashboards.go` : son dashboard.
4. `assets/central/prometheus/rules.yml.tmpl` et `assets/runbooks/` : ses alertes et leurs runbooks.

Les tests vérifient la cohérence de l'ensemble :
- chaque alerte d'un preset existe ;
- chaque alerte a son runbook ;
- chaque seuil a une valeur par défaut ;
- chaque série `observa:*` utilisée est bien enregistrée.

Lancez `make test test-rules`.
