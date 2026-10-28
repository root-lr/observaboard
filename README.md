# ObservaBoard

**Le monitoring de vos applications Docker en une commande.** ObservaBoard installe et configure Grafana, Prometheus, Loki, Tempo et Alertmanager, détecte la stack de chaque projet (Django, FastAPI, Celery, PostgreSQL, Redis…), l'instrumente sans toucher au code et génère dashboards, alertes et runbooks adaptés.

```text
observa central install --domain observa.example.com     # le cerveau : stockage, dashboards, alertes
observa agent install --central … --token …              # un agent par serveur à surveiller
observa init && observa apply                             # dans chaque projet : détection + configuration
```

Self-hosted, natif Docker (Compose ou `docker run`), multi-projets et multi-serveurs. Licence MIT.

---

## Essayer en 5 minutes (Mac, Windows ou Linux)

Prérequis : Docker (Docker Desktop sur Mac/Windows) et Go 1.24+ pour compiler la CLI.

```bash
git clone https://github.com/root-lr/observaboard && cd observaboard
make build
bin/observa up --all-in-one --with-example
```

Cette commande lance, sur votre machine :

- le **central** (Grafana sur http://localhost:3000, identifiants affichés) ;
- l'**agent** ;
- une **boutique Django d'exemple** (PostgreSQL, Redis, worker Celery) avec un générateur de trafic.

Après une à deux minutes, la *Vue d'ensemble* de Grafana se remplit. Provoquez ensuite une panne et regardez l'alerte se déclencher :

```bash
docker compose --project-directory ~/.observa/local/example exec web python manage.py chaos errors
#   autres pannes : latency · dependency · tasks · status · reset
```

Arrêter : `bin/observa down --all-in-one` (ajoutez `--volumes` pour effacer les données).

---

## Déployer pour de vrai

```text
              ┌──────────────── Serveur central (petit VPS dédié) ────────────────┐
              │ Caddy (HTTPS, tokens) → Prometheus · Loki · Tempo → Grafana        │
              │                         Prometheus → Alertmanager → email, Slack…  │
              └───────────────▲───────────────────▲───────────────────▲───────────┘
                              │      push HTTPS + token par agent      │
                   ┌──────────┴─────────┐ ┌───────┴────────────┐ ┌────┴───────────────┐
                   │ Serveur A          │ │ Serveur B          │ │ Serveur C          │
                   │ agent (Alloy)      │ │ agent (Alloy)      │ │ agent (Alloy)      │
                   │ projet1 · api/db   │ │ projet2 · api      │ │ projet1 · staging  │
                   └────────────────────┘ └────────────────────┘ └────────────────────┘
```

### 1. Le central

Sur un serveur dédié (2 vCPU, 4 Go de RAM, 50 Go de disque pour les rétentions par défaut), dont le nom de domaine pointe vers son IP :

```bash
sudo observa central install --domain observa.example.com --email vous@example.com
```

La commande affiche le mot de passe Grafana et le token du premier agent. Les deux ne sont montrés qu'une fois. Le certificat HTTPS est obtenu automatiquement (Let's Encrypt).

Sans nom de domaine, utilisez `--ip <adresse>` : Caddy émet alors un certificat interne. Récupérez son autorité avec `observa central ca > central-ca.crt`, puis passez ce fichier aux agents avec `--ca-file`.

Configurez les notifications dans `/opt/observa/central/central.yaml` (email, Slack, Telegram, webhook ; voir [docs/central-yaml.md](docs/central-yaml.md)), puis :

```bash
sudo observa central reload
sudo observa alert test          # vérifie qu'une notification arrive
```

### 2. Un agent par serveur

```bash
# sur le central : un token par serveur
sudo observa token create srv-a

# sur le serveur srv-a (le token est lu dans un fichier pour ne pas rester dans l'historique)
sudo observa agent install --central https://observa.example.com --token-file token.txt
```

L'agent est un unique conteneur Grafana Alloy. Il **pousse** ses données vers le central : aucun port à ouvrir sur vos serveurs. Il surveille immédiatement l'hôte et tous les conteneurs portant le label `observa.enable=true`.

### 3. Chaque projet

Dans le dépôt du projet (sur votre poste ou sur le serveur) :

```bash
observa init
```

`init` lit le `docker-compose.yml` et les dépendances (`requirements.txt`, `pyproject.toml`, `package.json`, Dockerfile), propose un preset par service, puis génère :

| Fichier | Rôle |
| --- | --- |
| `observa.yaml` | Description du projet : services, presets, seuils d'alerte, canaux de notification. À versionner. |
| `docker-compose.observa.yml` | Override Compose : labels, réseau `observa`, variables OpenTelemetry, commande instrumentée. Le compose d'origine n'est jamais modifié. |
| `Dockerfile.observa` | Image dérivée qui ajoute le SDK OpenTelemetry (Python ou Node.js). |

Puis, sur le serveur :

```bash
observa apply                                                          # seuils, routage, bases de données
docker compose -f docker-compose.yml -f docker-compose.observa.yml up -d --build
observa doctor                                                         # tout arrive-t-il ?
```

`observa init` affiche la commande exacte de votre projet. Si un `docker-compose.override.yml` existe, elle l'inclut, car Compose l'ignore dès qu'on passe `-f`.

`init` gère les cas courants :
- `ENTRYPOINT` (c'est lui qui est instrumenté) ;
- Dockerfiles multi-étapes avec `build.target` ;
- services en `network_mode: host` (surveillés sans réseau `observa`) ;
- variables `$` du Dockerfile.

Conteneurs lancés sans Compose : `observa init --runtime docker --service api=django --service db=postgres`. Conteneur déjà en marche, sans le redémarrer : `observa attach <conteneur> --project p --preset generic`.

---

## Ce qui est surveillé

| Niveau | Ce que vous faites | Ce que vous obtenez |
| --- | --- | --- |
| **1 · sans toucher l'app** | rien (label `observa.enable`) | logs centralisés, CPU/mémoire/réseau/redémarrages, disponibilité, métriques de l'hôte |
| **1 · eBPF** (option `--ebpf`, Linux) | déclarer `instrumentation: ebpf` et le port | traces HTTP et métriques HTTP, quel que soit le langage |
| **2 · démarrage modifié** | appliquer l'override généré | traces détaillées (vues, SQL, appels sortants, Redis, Celery), métriques HTTP par route |
| **3 · code** | ajouter vos métriques métier (SDK OpenTelemetry) | indicateurs métier (guide en V2) |

| Preset | Détecté par | Dashboard | Alertes par défaut (seuils modifiables) |
| --- | --- | --- | --- |
| `django` | dépendance `django`, `gunicorn`/`manage.py` | Service HTTP | 5xx > 5 % · p95 > 1 s · service arrêté |
| `fastapi` | dépendance `fastapi`/`starlette` | Service HTTP | 5xx > 5 % · p95 > 1 s · service arrêté |
| `celery` | commande `celery … worker` | Worker Celery | tâches en échec > 10 % · service arrêté |
| `postgres` | image `postgres`, `postgis`, `timescale` | PostgreSQL | injoignable · connexions > 80 % |
| `redis` | image `redis`, `valkey` | Redis | injoignable · mémoire > 90 % · file Celery > 100 |
| `generic` | tout le reste (Node.js instrumenté automatiquement) | Service · conteneur | arrêté · redémarrages en boucle · 5xx · p95 |

Toujours actives : agent silencieux, disque > 85 %, mémoire > 90 %, composant du central en panne, Watchdog (heartbeat externe optionnel). Chaque alerte renvoie vers un **runbook** (servi sur `/runbooks/`) et vers le dashboard de diagnostic.

Langages : tout conteneur est couvert au niveau 1. Le niveau 2 couvre **Python** et **Node.js** en V1, puis Java, PHP, .NET et Ruby en V2. Le détail est dans [docs/presets.md](docs/presets.md).

---

## Commandes

| Commande | Où | Rôle |
| --- | --- | --- |
| `observa up --all-in-one [--with-example]` | poste, serveur unique | central + agent sur la même machine |
| `observa down --all-in-one [--volumes]` | idem | tout arrêter |
| `observa central install --domain …` · `reload` · `upgrade` · `status` · `ca` | central | installer et gérer le central |
| `observa token create\|list\|revoke <agent>` | central | gérer les tokens des agents |
| `observa agent install --central … --token-file …` · `reload` · `status` | serveur | installer et gérer l'agent (`--token`, `--token-file`, ou `OBSERVA_AGENT_TOKEN`) |
| `observa agent env set NOM=valeur` | serveur | secrets de l'agent (accès PostgreSQL, Redis) |
| `observa init` | projet | détecter la stack, générer `observa.yaml` et l'override |
| `observa validate` | projet, CI | valider `observa.yaml` |
| `observa apply [--remove]` | serveur | enregistrer le projet auprès de l'agent |
| `observa attach <conteneur>` · `detach` | serveur | surveiller un conteneur existant sans labels |
| `observa preset list\|show <nom>` | partout | presets disponibles |
| `observa doctor` | partout | diagnostiquer agent, central et projet |
| `observa alert test` | central | envoyer une alerte de test |

Option globale `--dry-run` : affiche les commandes Docker au lieu de les exécuter.

---

## Documentation

- [Architecture et flux de données](docs/architecture.md)
- [Référence observa.yaml](docs/observa-yaml.md) · [Référence central.yaml](docs/central-yaml.md)
- [Presets et langages](docs/presets.md)
- [Sécurité](docs/securite.md)
- [Mac et Windows](docs/mac-windows.md)
- [Développement : structure, tests, versions](docs/developpement.md)
- [Feuille de route V2](docs/v2.md)

## Limites connues (V1)

- **Taux d'échantillonnage :** les métriques HTTP sont calculées à partir des traces. Il faut donc échantillonner à 100 %, ce qui est le réglage par défaut, adapté aux trafics modestes à moyens.
- **Dashboards :** ils sont partagés entre les projets, avec un filtre projet/env/service. Le dossier par projet arrive en V2.
- **Agent :** il tourne en conteneur privilégié (exigence de cAdvisor) et lit le socket Docker. Les services de tous les projets d'un serveur partagent le réseau `observa`. Voir [docs/securite.md](docs/securite.md).
- **eBPF :** il nécessite un noyau Linux 5.8+. Sous Docker Desktop, il n'est pas garanti.
- **Celery :** seule la file `celery` (base 0) est surveillée.
- **Non couvert :** Kubernetes, serverless et mobile ([PRD](docs/v2.md)).

## Licence

MIT, voir [LICENSE](LICENSE). Les dépendances embarquées dans `internal/thirdparty/` conservent leur licence d'origine (MIT/Apache-2.0, BSD-3-Clause).
