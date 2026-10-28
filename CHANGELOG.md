# Changelog

## 0.1.0 — V1 (2026-10-28)

Première version : CLI `observa` et stack complète.

- **Central :** Caddy (HTTPS automatique, authentification des agents par token), Prometheus, Loki, Tempo (span metrics), Alertmanager (routage par projet et sévérité, inhibitions, heartbeat), Grafana (sources et dashboards provisionnés).
- **Agent :** Grafana Alloy, avec découverte Docker par labels, logs, cAdvisor, métriques de l'hôte, récepteur OTLP, eBPF optionnel, exporters PostgreSQL et Redis. L'agent pousse vers le central, et un tampon disque protège des coupures.
- **Projets :**
  - `observa init` : détection de stack, `observa.yaml`, override Compose, image dérivée avec OpenTelemetry pour Python et Node.js ;
  - `observa apply` : seuils et routage publiés par l'agent ;
  - `observa attach` pour les conteneurs existants ;
  - mode `docker run`.
- **Presets :** django, fastapi, celery, postgres, redis, generic. 7 dashboards, 14 alertes avec leurs runbooks.
- **All-in-one :** `observa up --all-in-one --with-example` lance la boutique Django d'exemple, un générateur de trafic et des pannes simulées.
- **Outils :** `observa doctor`, `observa alert test`, `--dry-run`.
- **CI :** tests Go, tests promtool des règles, validation des configs avec les binaires officiels, test de bout en bout, binaires de release.
