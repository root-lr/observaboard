#!/usr/bin/env bash
# ObservaBoard end-to-end test (run by CI, or locally with `make e2e`).
#
#  1. starts central + agent + example shop (observa up --all-in-one --with-example)
#  2. checks that metrics, logs, traces and dashboards reach Grafana
#  3. triggers a failure (chaos errors) and waits for the ObservaHighErrorRate alert
#  4. stops everything and deletes the data
#
# Usage: tests/e2e/run.sh [path/to/observa]
set -euo pipefail

OBSERVA=${1:-bin/observa}
OBSERVA=$(cd "$(dirname "$OBSERVA")" && pwd)/$(basename "$OBSERVA")
DIR=$(mktemp -d)
export OBSERVA_LOCAL_DIR="$DIR"
URL=http://127.0.0.1:3000

log()  { printf '\n\033[1m== %s\033[0m\n' "$*"; }
fail() { printf '\n\033[31mÉCHEC : %s\033[0m\n' "$*"; dump; exit 1; }

dump() {
  echo "--- diagnostics"
  "$OBSERVA" doctor --agent-dir "$DIR/agent" --central-dir "$DIR/central" --project-dir "$DIR/example" || true
  docker compose --project-directory "$DIR/central" -f "$DIR/central/docker-compose.yml" ps || true
  for c in observa-agent observa-tempo observa-loki observa-prometheus observa-grafana observa-caddy; do
    echo "--- logs $c"; docker logs --tail 40 "$c" 2>&1 || true
  done
}

cleanup() {
  log "Arrêt"
  "$OBSERVA" down --all-in-one --volumes || true
  rm -rf "$DIR"
}
trap cleanup EXIT

# grafana_api <path>: authenticated call to the Grafana API
grafana_api() {
  local pw
  pw=$(grep GF_SECURITY_ADMIN_PASSWORD "$DIR/central/.env" | cut -d= -f2)
  curl -fsS -u "admin:$pw" "$URL$1"
}

# prom <PromQL query>: number of series returned (via the Grafana proxy)
prom() {
  grafana_api "/api/datasources/proxy/uid/prometheus/api/v1/query?query=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1]))' "$1")" \
    | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["data"]["result"]))'
}

# wait_for <description> <timeout in s> <command...>: retries until success
wait_for() {
  local what=$1 timeout=$2; shift 2
  local start=$SECONDS
  until "$@" >/dev/null 2>&1; do
    (( SECONDS - start > timeout )) && fail "$what (après ${timeout}s)"
    sleep 10
  done
  echo "✓ $what ($((SECONDS - start))s)"
}

has_series() { [ "$(prom "$1")" -gt 0 ]; }
has_logs() {
  grafana_api "/api/datasources/proxy/uid/loki/loki/api/v1/query_range?query=$(python3 -c 'import urllib.parse;print(urllib.parse.quote("{project=\"boutique\", service=\"web\"}"))')&limit=5" \
    | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["data"]["result"] else 1)'
}
has_traces() {
  grafana_api "/api/datasources/proxy/uid/tempo/api/search?tags=service.name%3Dweb&limit=5" \
    | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("traces") else 1)'
}
has_dashboard() { grafana_api "/api/dashboards/uid/$1" >/dev/null; }

log "Démarrage all-in-one avec la boutique d'exemple"
"$OBSERVA" up --all-in-one --with-example || fail "observa up"

log "Les données arrivent"
wait_for "Grafana répond"                                  180 curl -fsS "$URL/api/health"
wait_for "dashboards provisionnés"                          120 has_dashboard observa-http
wait_for "métriques de l'hôte"                             240 has_series 'node_boot_time_seconds{job="observa/host"}'
wait_for "métriques des conteneurs"                         240 has_series 'container_last_seen{project="boutique", service="web"}'
wait_for "seuils publiés par observa apply"                 240 has_series 'observa_service_threshold{project="boutique", service="web"}'
wait_for "métriques HTTP depuis les traces (span metrics)"  420 has_series 'traces_spanmetrics_calls_total{project="boutique", service="web", span_kind="SPAN_KIND_SERVER"}'
wait_for "règle observa:http_requests:rate5m"               300 has_series 'observa:http_requests:rate5m{project="boutique", service="web"}'
wait_for "métriques PostgreSQL"                             240 has_series 'pg_up{project="boutique", service="db"} == 1'
wait_for "métriques Redis"                                  240 has_series 'redis_up{project="boutique", service="redis"} == 1'
wait_for "tâches Celery (traces du worker)"                 420 has_series 'traces_spanmetrics_calls_total{project="boutique", service="worker", span_kind="SPAN_KIND_CONSUMER"}'
wait_for "logs du service web dans Loki"                    240 has_logs
wait_for "traces du service web dans Tempo"                 240 has_traces

log "Diagnostic"
"$OBSERVA" doctor --agent-dir "$DIR/agent" --central-dir "$DIR/central" --project-dir "$DIR/example" || fail "observa doctor"

log "Panne simulée : 30 % d'erreurs 500 sur /api/products/"
docker compose --project-directory "$DIR/example" -f "$DIR/example/compose.yaml" -f "$DIR/example/docker-compose.observa.yml" \
  exec -T web python manage.py chaos errors --minutes 15
wait_for "alerte ObservaHighErrorRate déclenchée" 900 has_series 'ALERTS{alertname="ObservaHighErrorRate", alertstate="firing", project="boutique", service="web"}'

log "Runbook servi par Caddy"
curl -fsS "$URL/runbooks/observa-high-error-rate" | grep -q "ObservaHighErrorRate" || fail "runbook introuvable"
echo "✓ runbook disponible"

log "Succès"
