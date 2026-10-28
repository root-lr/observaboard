# Développement

## Structure du dépôt

```text
cmd/observa/              point d'entrée de la CLI
internal/
  cli/                    commandes (un fichier par famille : central, agent, project, up, doctor)
  central/                réglages du central, tokens, rendu de toutes les configurations, Alertmanager
  agent/                  réglages de l'agent, projets appliqués, attach, rendu de config.alloy, seuils
  project/                modèle et validation de observa.yaml
  compose/                lecture des fichiers compose et des Dockerfiles
  detect/                 détection preset/runtime/port par service
  initgen/                génération de l'override compose et des Dockerfiles dérivés
  instrument/             variables OpenTelemetry, commande instrumentée, lignes Dockerfile
  presets/                connaissances par technologie (seuils, alertes, dashboards)
  dashboards/             dashboards Grafana écrits en Go (builder + définitions)
  runbook/                conversion Markdown → HTML des runbooks
  dockerx/                appels docker / docker compose (mode --dry-run)
  fsutil/                 écriture atomique de fichiers
  version/                version de la CLI et images épinglées
  thirdparty/             yaml.v3 et bcrypt embarqués (voir leur README)
assets/
  central/                templates : compose, Caddyfile, Prometheus (+ règles), Loki, Tempo, Grafana
  agent/                  templates : config.alloy, compose de l'agent
  runbooks/               un runbook Markdown par alerte
examples/django-shop/     boutique d'exemple (Django, PostgreSQL, Redis, Celery, trafic, pannes simulées)
tests/rules/              tests unitaires promtool des règles d'alerte
tests/e2e/                test de bout en bout (Docker requis)
```

Le module Go s'appelle `github.com/root-lr/observaboard`. Si vous publiez sous un autre compte, remplacez ce chemin partout : `go.mod`, imports, README, `install.sh`.

## Tests

| Commande | Ce qu'elle vérifie | Prérequis |
| --- | --- | --- |
| `make test` | `go vet` et tests Go : validation, détection sur l'exemple, override généré, rendu agent et central, YAML/JSON valides, cohérence alertes ↔ runbooks ↔ presets ↔ seuils, dashboards, routage Alertmanager, tokens, parcours CLI complets en `--dry-run` | Go |
| `make test-rules` | comportement des alertes avec `promtool test rules` : seuils par défaut et surchargés, routage, service arrêté, agent silencieux, PostgreSQL, file Celery, tâches en échec | promtool ou Docker |
| `make test-configs` | chaque configuration générée validée par le binaire officiel du composant, aux versions épinglées : promtool, amtool, `caddy validate`, `loki -verify-config`, `alloy fmt`, `docker compose config` | Docker |
| `make e2e` | stack réelle, application d'exemple, arrivée des métriques/logs/traces, panne simulée, déclenchement de l'alerte | Docker, 15 min |

La CI GitHub Actions (`.github/workflows/ci.yml`) exécute les quatre. Un tag `v*` publie les binaires.

### Revue de code avant livraison

Une revue indépendante a relevé 2 bugs bloquants et 11 importants, tous corrigés et couverts par des tests :
- **Bloquant :** montage de fichiers seuls (rechargements à chaud inopérants).
- **Bloquant :** droits de lecture d'`alertmanager.yml`.
- **Génération :** `network_mode`, Dockerfiles multi-étapes avec `target` et `USER` hérité, `ENTRYPOINT` à instrumenter, `$` du Dockerfile à échapper, noms de services avec `_`, override Compose implicite.
- **Agent et CLI :** valeurs du `.env` à mettre entre apostrophes, dossiers par défaut après un all-in-one, options lors d'une réinstallation, routage contradictoire entre deux agents, port eBPF en double, IPv6, `localhost`, email, lecture de l'entrée standard.

### Ce qui a été vérifié lors de l'écriture de la V1

L'environnement de développement n'avait pas de démon Docker. Les vérifications suivantes ont donc été faites avec les binaires disponibles :

- **Tests :** tests Go et 8 scénarios `promtool test rules` passent. Les tests promtool ont été vérifiés par mutation (une attente faussée les fait échouer).
- **Validation des configs :**
  - `promtool check rules` : 31 règles ;
  - `amtool check-config` et `amtool config routes test` : routage par canal, défaut, heartbeat ;
  - `caddy validate` : Caddy 2.11.7, version exacte ;
  - parseur officiel d'Alloy 1.21.2 sur les `config.alloy` générés ;
  - `docker compose config` sur la fusion compose d'origine + override ;
  - 138 expressions PromQL analysées par un parseur PromQL.
- **Chaîne réelle :** agent Prometheus et node_exporter → Caddy (token) → Prometheus central avec les règles → Alertmanager. Vérifiés : refus sans token ou avec un mauvais token, acceptation du bon, révocation à chaud, runbooks servis, toutes les règles évaluées sans erreur, routage calculé.
- **Instrumentation réelle :** la boutique avec 3 workers gunicorn, lancée avec exactement les variables et la commande générées. Vérifiés : tous les spans reçus, attributs `observa.*`, `http.route`, `http.response.status_code`, spans SQL, healthcheck exclu, `trace_id` dans les logs.

**Non exécuté ici, couvert par `make test-configs` et `make e2e` en CI :** démarrage effectif de Loki, Tempo, Grafana et Alloy dans Docker, et validation des arguments des composants Alloy au-delà de la syntaxe.

## Mettre à jour un composant

1. Changez la version dans `internal/version/version.go`, et dans le `Makefile` pour les images de validation.
2. Lisez le changelog du composant. Tempo 3 et Alloy changent régulièrement de syntaxe : le dépôt du composant, au tag visé, fait foi.
3. Lancez `make test test-rules test-configs e2e`.

## Conventions

- **Messages :** les messages de la CLI et la documentation sont en français ; le code et les commentaires sont en anglais.
- **Fichiers générés :** ils commencent par un commentaire « Généré par… ne pas modifier ».
- **Écritures :** toute écriture passe par `fsutil.WriteFile` (atomique, et indique si le contenu a changé), ce qui permet de ne recharger que ce qui a changé.
- **Secrets :** ils vont dans des fichiers en mode 0600 et ne sont jamais affichés deux fois.
