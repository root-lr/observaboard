# Architecture

ObservaBoard sépare **le central**, qui stocke, affiche et alerte, et **les agents**, un par serveur, qui collectent et poussent. La CLI `observa` génère et maintient toute la configuration des deux côtés.

## Composants

| Côté | Composant | Version | Rôle |
| --- | --- | --- | --- |
| Central | Caddy | 2.11 | Seul port exposé : HTTPS (Let's Encrypt ou CA interne), authentification des agents, runbooks, proxy vers Grafana |
| Central | Prometheus | 3.15 | Reçoit les métriques (remote write), évalue les règles d'alerte |
| Central | Loki | 3.7 | Stocke les logs |
| Central | Tempo | 3.1 (mono-binaire) | Stocke les traces, **calcule les métriques HTTP et Celery à partir des traces** |
| Central | Alertmanager | 0.34 | Regroupe et route les alertes vers les canaux |
| Central | Grafana | 13.3 | Dashboards, exploration, corrélation logs ↔ traces |
| Agent | Grafana Alloy | 1.21 | Découverte Docker, logs, cAdvisor, métriques hôte, récepteur OTLP, eBPF (Beyla), exporters PostgreSQL et Redis |

Les versions sont épinglées dans `internal/version/version.go`.

## Flux de données

```text
Application (SDK OpenTelemetry, niveau 2) ──OTLP/HTTP──▶ agent :4318 (réseau Docker `observa`)
Conteneurs (socket Docker) ──────────────────────────────▶ agent : logs + cAdvisor
Hôte (/proc, /sys, textfile) ────────────────────────────▶ agent : métriques hôte, seuils, routage
PostgreSQL / Redis (réseau `observa`) ───────────────────▶ agent : exporters

agent ──HTTPS + basic auth (agent, token)──▶ Caddy ─┬─ /ingest/prometheus/write ─▶ Prometheus (remote write)
                                                    ├─ /ingest/loki/push ────────▶ Loki
                                                    └─ /ingest/otlp/v1/traces ───▶ Tempo
Tempo (metrics generator : span metrics, graphe de services) ──remote write──▶ Prometheus
Prometheus (règles) ──▶ Alertmanager ──▶ email · Slack · Telegram · webhook · heartbeat
```

L'agent bufferise sur disque (WAL) : une coupure réseau de quelques heures ne perd pas les métriques.

## Les labels, épine dorsale

Chaque série, ligne de log et trace porte les mêmes étiquettes :

| Label | Origine |
| --- | --- |
| `project`, `env`, `service`, `preset` | labels Docker `observa.*` (posés par l'override ou `observa attach`), attributs de ressource `observa.*` (OpenTelemetry) |
| `host` | label externe ajouté par l'agent (`--host`, nom du serveur par défaut) |
| `container` | nom du conteneur |

Les dashboards filtrent sur ces labels. Les règles d'alerte agrègent par `project, env, service`. Le routage des notifications utilise les labels `notify_critical` et `notify_warning`, joints aux alertes.

## Pourquoi les métriques HTTP viennent des traces

Les SDK OpenTelemetry savent exporter des métriques HTTP, mais ces métriques deviennent fausses avec les serveurs multi-processus comme gunicorn ou uvicorn avec workers. Chaque worker exporte en effet les mêmes séries, et leurs compteurs se mélangent. ObservaBoard désactive donc les métriques des SDK (`OTEL_METRICS_EXPORTER=none`) et fait calculer par Tempo des **span metrics** à partir des spans serveur :

- `traces_spanmetrics_calls_total{project, env, service, span_kind, status_code, http_request_method, http_route, http_response_status_code}`
- `traces_spanmetrics_latency_bucket{…, le}`

C'est juste quel que soit le nombre de workers. C'est identique pour tous les langages et pour les traces eBPF. Et ça donne les dépendances (SQL, appels sortants) sans configuration. La contrepartie : il faut échantillonner les traces à 100 %, le réglage par défaut. `project` et `env` sont posés par les `dimension_mappings` de Tempo à partir des attributs `observa.project` et `observa.env`.

## Seuils et routage sans configuration centrale

`observa apply` écrit sur l'agent un fichier lu par le *textfile collector* :

```text
observa_service_info{project="wulo",env="prod",service="api",preset="django",notify_critical="telegram-ops",notify_warning="slack"} 1
observa_service_threshold{project="wulo",env="prod",service="api",threshold="http_latency_p95_seconds"} 0.8
```

Les règles du central combinent ces séries avec des valeurs par défaut :

```promql
observa:threshold:http_latency_p95_seconds =
  max by (project, env, service) (observa_service_threshold{threshold="http_latency_p95_seconds"})
  or on (project, env, service) (observa:service:present * 0 + 1)
```

Chaque alerte est multipliée par `observa:service:routing`, qui vaut 1, avec `group_left (notify_critical, notify_warning)`. Les labels de routage s'ajoutent ainsi sans changer la valeur. Alertmanager route ensuite par expression régulière sur ces labels (`notify_critical=~"(.+,)?telegram-ops(,.+)?"`). Les canaux eux-mêmes, avec leurs secrets, restent définis uniquement sur le central.

Résultat : un projet modifie ses seuils et ses destinataires dans son dépôt, sans toucher au central ni redéployer de règles.

## Réseau Docker `observa`

L'agent et les services surveillés partagent un réseau bridge externe, `observa`. L'override ajoute chaque service à ce réseau avec un alias unique (`<projet>-<env>-<service>`), en plus de son réseau d'origine. Cela permet :

- aux applications d'envoyer leurs traces à `http://observa-agent:4318` ;
- aux exporters de l'agent de joindre `wulo-prod-db:5432` sans publier de port.

## Fichiers générés

| Central (`/opt/observa/central`) | Agent (`/opt/observa/agent`) |
| --- | --- |
| `central.yaml` (réglages, canaux, 0600) | `agent.yaml` (réglages) |
| `tokens.yaml` (hachés bcrypt, 0600) | `.env` (token, secrets des exporters, 0600) |
| `.env` (mot de passe admin Grafana initial) | `config.alloy` (généré) |
| `docker-compose.yml`, `caddy/`, `prometheus/`, `alertmanager/`, `loki/`, `tempo/`, `grafana/`, `runbooks/` (générés) | `docker-compose.yml`, `config/config.alloy` (+ `config/ca.crt`), `projects/*.yaml`, `attach.yaml`, `textfile/*.prom` |

Les conteneurs montent des **dossiers**, jamais des fichiers seuls. Les fichiers sont réécrits de façon atomique, donc par remplacement. Un fichier monté seul resterait sur l'ancienne version, et les rechargements à chaud (Caddy, Prometheus, Alertmanager, Alloy) ne verraient pas les changements.

Le dossier du central est en mode 0700, car il contient les secrets. Les conteneurs n'en voient que les sous-dossiers montés.

Les fichiers générés ne doivent pas être modifiés à la main. Modifiez `central.yaml` ou `observa.yaml`, puis lancez `observa central reload` ou `observa apply`.
