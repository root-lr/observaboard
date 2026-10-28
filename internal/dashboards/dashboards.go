package dashboards

import "fmt"

// Folders used in Grafana.
const (
	FolderMain    = "ObservaBoard"
	FolderPresets = "Presets"
)

// All returns every dashboard shipped with ObservaBoard.
func All() []*Dashboard {
	return []*Dashboard{Overview(), Host(), HTTP(), Container(), Celery(), Postgres(), Redis()}
}

// Path is the file path of a dashboard under the provisioning directory.
func (d *Dashboard) Path() string { return d.Folder + "/" + d.UID + ".json" }

var (
	varProjectMulti = Var{Name: "project", Label: "Projet", Multi: true, Query: `label_values(observa:service:present, project)`}
	varEnvMulti     = Var{Name: "env", Label: "Environnement", Multi: true, Query: `label_values(observa:service:present{project=~"$project"}, env)`}
)

const pe = `project=~"$project", env=~"$env"`                    // multi-select scope
const pes = `project="$project", env="$env", service="$service"` // single service scope

func links() []Link {
	return []Link{
		{Title: "Vue d'ensemble", URL: "/d/observa-overview"},
		{Title: "Serveurs", URL: "/d/observa-host"},
		{Title: "Services HTTP", URL: "/d/observa-http"},
		{Title: "Conteneurs", URL: "/d/observa-container"},
		{Title: "Runbooks", URL: "/runbooks/"},
	}
}

// Overview is the home dashboard.
func Overview() *Dashboard {
	d := &Dashboard{
		UID: "observa-overview", Title: "ObservaBoard · Vue d'ensemble", Folder: FolderMain,
		Description: "État de tous les projets, services et serveurs surveillés.",
		Vars:        []Var{varProjectMulti, varEnvMulti},
		Links:       links(),
	}
	d.Row("État général")
	d.Add(
		Panel{Type: "stat", Title: "Serveurs connectés", Width: 4, Height: 4,
			Description: "Agents ayant envoyé des données dans les 5 dernières minutes.",
			Targets:     []Target{{Expr: `count(count by (host) (node_boot_time_seconds{job="observa/host"})) or vector(0)`}}},
		Panel{Type: "stat", Title: "Services surveillés", Width: 4, Height: 4,
			Targets: []Target{{Expr: `count(observa:service:present{` + pe + `}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Services arrêtés", Width: 4, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "red", Value: F(1)}},
			Targets:    []Target{{Expr: `count((time() - observa:service:last_seen_timestamp{` + pe + `}) > 120) or vector(0)`}}},
		Panel{Type: "stat", Title: "Alertes en cours", Width: 4, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(1)}},
			Targets:    []Target{{Expr: `count(ALERTS{alertstate="firing", severity!="none"}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Requêtes HTTP / s", Width: 4, Height: 4, Unit: "reqps",
			Targets: []Target{{Expr: `sum(observa:http_requests:rate5m{` + pe + `}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Réponses 5xx", Width: 4, Height: 4, Unit: "percentunit", Decimals: I(2),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.01)}, {Color: "red", Value: F(0.05)}},
			Targets:    []Target{{Expr: `(sum(observa:http_errors:rate5m{` + pe + `}) or vector(0)) / sum(observa:http_requests:rate5m{` + pe + `})`}}},
	)
	d.Row("Services")
	d.Add(
		Panel{Type: "table", Title: "Services", Width: 24, Height: 9,
			Description: "Trafic, erreurs et latence par service (5 dernières minutes). Cliquez sur un service pour ouvrir son dashboard.",
			Targets: []Target{
				{Expr: `max by (project, env, service) (time() - observa:service:last_seen_timestamp{` + pe + `})`, Instant: true, Table: true},
				{Expr: `observa:http_requests:rate5m{` + pe + `}`, Instant: true, Table: true},
				{Expr: `observa:http_error_ratio:rate5m{` + pe + `}`, Instant: true, Table: true},
				{Expr: `observa:http_latency_p95:5m{` + pe + `}`, Instant: true, Table: true},
			},
			Rename: map[string]string{
				"project": "Projet", "env": "Env", "service": "Service",
				"Value #A": "Vu il y a (s)", "Value #B": "Req/s", "Value #C": "5xx", "Value #D": "p95 (s)",
			},
			Overrides: []map[string]any{
				fieldUnit("Value #C", "percentunit"),
				fieldUnit("Value #D", "s"),
				fieldUnit("Value #A", "s"),
				fieldUnit("Value #B", "reqps"),
			},
			CellLinks: map[string]string{
				"service": "/d/observa-container?var-project=${__data.fields.project}&var-env=${__data.fields.env}&var-service=${__value.raw}&${__url_time_range}",
			},
		},
		Panel{Type: "timeseries", Title: "Part de réponses 5xx par service", Width: 12, Unit: "percentunit", Min: F(0),
			Targets: []Target{{Expr: `observa:http_error_ratio:rate5m{` + pe + `}`, Legend: "{{project}}/{{env}}/{{service}}"}}},
		Panel{Type: "timeseries", Title: "Latence p95 par service", Width: 12, Unit: "s", Min: F(0),
			Targets: []Target{{Expr: `observa:http_latency_p95:5m{` + pe + `}`, Legend: "{{project}}/{{env}}/{{service}}"}}},
		Panel{Type: "table", Title: "Alertes en cours", Width: 24, Height: 7,
			Targets: []Target{{Expr: `ALERTS{alertstate="firing", severity!="none"}`, Instant: true, Table: true}},
			Hide:    []string{"Value", "__name__", "alertstate", "notify_critical", "notify_warning"},
			Rename:  map[string]string{"alertname": "Alerte", "severity": "Sévérité", "project": "Projet", "env": "Env", "service": "Service", "host": "Serveur"},
		},
	)
	d.Row("Serveurs")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU par serveur", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - avg by (host) (rate(node_cpu_seconds_total{job="observa/host", mode="idle"}[5m]))`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Mémoire par serveur", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - node_memory_MemAvailable_bytes{job="observa/host"} / node_memory_MemTotal_bytes{job="observa/host"}`, Legend: "{{host}}"}}},
	)
	return d
}

func fieldUnit(field, unit string) map[string]any {
	return map[string]any{
		"matcher":    map[string]any{"id": "byName", "options": field},
		"properties": []map[string]any{{"id": "unit", "value": unit}},
	}
}

// Host shows server resources.
func Host() *Dashboard {
	d := &Dashboard{
		UID: "observa-host", Title: "ObservaBoard · Serveurs", Folder: FolderMain,
		Description: "Ressources des serveurs équipés d'un agent, et conteneurs les plus gourmands.",
		Vars:        []Var{{Name: "host", Label: "Serveur", Multi: true, Query: `label_values(node_boot_time_seconds{job="observa/host"}, host)`}},
		Links:       links(),
	}
	h := `job="observa/host", host=~"$host"`
	fs := h + `, fstype!~"tmpfs|overlay|squashfs|nsfs|ramfs|fuse.*", mountpoint!~"/(run|var/lib/docker|snap)($|/.*)"`
	d.Row("Ressources")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU utilisé", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - avg by (host) (rate(node_cpu_seconds_total{` + h + `, mode="idle"}[5m]))`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Charge (load 5 min) par cœur", Width: 12, Min: F(0),
			Targets: []Target{{Expr: `node_load5{` + h + `} / on (host) count by (host) (node_cpu_seconds_total{` + h + `, mode="idle"})`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Mémoire utilisée", Width: 12, Unit: "bytes", Min: F(0),
			Targets: []Target{
				{Expr: `node_memory_MemTotal_bytes{` + h + `} - node_memory_MemAvailable_bytes{` + h + `}`, Legend: "{{host}} utilisée"},
				{Expr: `node_memory_MemTotal_bytes{` + h + `}`, Legend: "{{host}} totale"},
			}},
		Panel{Type: "timeseries", Title: "Disques remplis", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - node_filesystem_avail_bytes{` + fs + `} / node_filesystem_size_bytes{` + fs + `}`, Legend: "{{host}} {{mountpoint}}"}}},
		Panel{Type: "timeseries", Title: "Réseau", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (host) (rate(node_network_receive_bytes_total{` + h + `, device!~"lo|veth.*|br-.*|docker.*"}[5m]))`, Legend: "{{host}} reçu"},
				{Expr: `-sum by (host) (rate(node_network_transmit_bytes_total{` + h + `, device!~"lo|veth.*|br-.*|docker.*"}[5m]))`, Legend: "{{host}} envoyé"},
			}},
		Panel{Type: "timeseries", Title: "Débit disque", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (host) (rate(node_disk_read_bytes_total{` + h + `}[5m]))`, Legend: "{{host}} lecture"},
				{Expr: `-sum by (host) (rate(node_disk_written_bytes_total{` + h + `}[5m]))`, Legend: "{{host}} écriture"},
			}},
	)
	d.Row("Conteneurs")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU des 10 conteneurs les plus actifs", Width: 12, Unit: "short",
			Description: "En cœurs utilisés.",
			Targets:     []Target{{Expr: `topk(10, sum by (host, container) (rate(container_cpu_usage_seconds_total{host=~"$host", container!=""}[5m])))`, Legend: "{{host}} {{container}}"}}},
		Panel{Type: "timeseries", Title: "Mémoire des 10 conteneurs les plus gourmands", Width: 12, Unit: "bytes",
			Targets: []Target{{Expr: `topk(10, sum by (host, container) (container_memory_working_set_bytes{host=~"$host", container!=""}))`, Legend: "{{host}} {{container}}"}}},
	)
	return d
}

func serviceVars(metric string) []Var {
	return []Var{
		{Name: "project", Label: "Projet", Query: fmt.Sprintf(`label_values(%s, project)`, metric)},
		{Name: "env", Label: "Environnement", Query: fmt.Sprintf(`label_values(%s{project="$project"}, env)`, metric)},
		{Name: "service", Label: "Service", Query: fmt.Sprintf(`label_values(%s{project="$project", env="$env"}, service)`, metric)},
	}
}

const logsSel = `{project="$project", env="$env", service="$service"}`
const errorFilter = ` |~ "(?i)(error|exception|traceback|fatal|critical)"`

func logPanels(d *Dashboard) {
	d.Row("Logs")
	d.Add(
		Panel{Type: "timeseries", Loki: true, Title: "Volume de logs", Width: 12, Height: 6, Bars: true, Stack: true,
			Targets: []Target{{Expr: `sum(count_over_time(` + logsSel + ` [1m]))`, Legend: "toutes les lignes"}},
		},
		Panel{Type: "timeseries", Loki: true, Title: "Lignes d'erreur", Width: 12, Height: 6, Bars: true,
			Targets: []Target{{Expr: `sum(count_over_time(` + logsSel + errorFilter + ` [1m]))`, Legend: "erreurs"}},
		},
		Panel{Type: "logs", Title: "Logs du service", Width: 24, Height: 12,
			Description: "Cliquez sur une ligne contenant trace_id pour ouvrir la trace correspondante.",
			Targets:     []Target{{Expr: logsSel}}},
	)
}

// HTTP covers Django, FastAPI and any service exposing OpenTelemetry HTTP metrics.
func HTTP() *Dashboard {
	d := &Dashboard{
		UID: "observa-http", Title: "Service HTTP (Django, FastAPI…)", Folder: FolderPresets,
		Description: "Trafic, erreurs, latence, routes, dépendances, traces et logs d'un service HTTP instrumenté. Métriques calculées à partir des traces (span metrics Tempo).",
		Vars:        serviceVars("observa:http_requests:rate5m"),
		Links:       links(),
	}
	sel := `{` + pes + `}`
	srv := `{` + pes + `, span_kind="SPAN_KIND_SERVER"}`
	srv5xx := `{` + pes + `, span_kind="SPAN_KIND_SERVER", http_response_status_code=~"5.."}`
	calls := `traces_spanmetrics_calls_total`
	lat := `traces_spanmetrics_latency_bucket`
	d.Row("Trafic")
	d.Add(
		Panel{Type: "stat", Title: "Requêtes / s", Width: 6, Height: 4, Unit: "reqps",
			Targets: []Target{{Expr: `observa:http_requests:rate5m` + sel}}},
		Panel{Type: "stat", Title: "Réponses 5xx", Width: 6, Height: 4, Unit: "percentunit", Decimals: I(2),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.01)}, {Color: "red", Value: F(0.05)}},
			Targets:    []Target{{Expr: `observa:http_error_ratio:rate5m` + sel}}},
		Panel{Type: "stat", Title: "Latence p95", Width: 6, Height: 4, Unit: "s",
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.5)}, {Color: "red", Value: F(1)}},
			Targets:    []Target{{Expr: `observa:http_latency_p95:5m` + sel}}},
		Panel{Type: "stat", Title: "Latence p99", Width: 6, Height: 4, Unit: "s",
			Targets: []Target{{Expr: `histogram_quantile(0.99, sum by (le) (rate(` + lat + srv + `[5m])))`}}},
		Panel{Type: "timeseries", Title: "Requêtes / s par classe de statut", Width: 12, Unit: "reqps", Stack: true,
			Targets: []Target{{Expr: `sum by (code) (label_replace(rate(` + calls + srv + `[5m]), "code", "${1}xx", "http_response_status_code", "([0-9]).*"))`, Legend: "{{code}}"}}},
		Panel{Type: "timeseries", Title: "Latence", Width: 12, Unit: "s", Min: F(0),
			Targets: []Target{
				{Expr: `histogram_quantile(0.50, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p50"},
				{Expr: `histogram_quantile(0.95, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p95"},
				{Expr: `histogram_quantile(0.99, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p99"},
				{Expr: `observa:threshold:http_latency_p95_seconds` + sel, Legend: "seuil d'alerte p95"},
			}},
		Panel{Type: "timeseries", Title: "Part de réponses 5xx et seuil", Width: 12, Unit: "percentunit", Min: F(0),
			Targets: []Target{
				{Expr: `observa:http_error_ratio:rate5m` + sel, Legend: "5xx"},
				{Expr: `observa:threshold:http_error_ratio` + sel, Legend: "seuil d'alerte"},
			}},
		Panel{Type: "timeseries", Title: "Requêtes en erreur par route", Width: 12, Unit: "reqps",
			Targets: []Target{{Expr: `topk(10, sum by (http_request_method, http_route) (rate(` + calls + srv5xx + `[5m])))`, Legend: "{{http_request_method}} {{http_route}}"}}},
	)
	d.Row("Routes")
	d.Add(
		Panel{Type: "table", Title: "Routes (5 dernières minutes)", Width: 24, Height: 9,
			Targets: []Target{
				{Expr: `sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m]))`, Instant: true, Table: true},
				{Expr: `histogram_quantile(0.95, sum by (http_request_method, http_route, le) (rate(` + lat + srv + `[5m])))`, Instant: true, Table: true},
				{Expr: `(sum by (http_request_method, http_route) (rate(` + calls + srv5xx + `[5m])) or sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m])) * 0) / sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m]))`, Instant: true, Table: true},
			},
			Rename: map[string]string{"http_request_method": "Méthode", "http_route": "Route", "Value #A": "Req/s", "Value #B": "p95 (s)", "Value #C": "5xx"},
			Overrides: []map[string]any{
				fieldUnit("Value #A", "reqps"), fieldUnit("Value #B", "s"), fieldUnit("Value #C", "percentunit"),
			},
		},
		Panel{Type: "timeseries", Title: "Routes les plus lentes (p95)", Width: 24, Unit: "s",
			Targets: []Target{{Expr: `topk(10, histogram_quantile(0.95, sum by (http_request_method, http_route, le) (rate(` + lat + srv + `[5m]))))`, Legend: "{{http_request_method}} {{http_route}}"}}},
	)
	sm := pes
	d.Row("Dépendances (calculées depuis les traces)")
	d.Add(
		Panel{Type: "timeseries", Title: "Temps passé en base de données", Width: 8, Unit: "percentunit", Min: F(0),
			Description: "Part du temps (en secondes par seconde) passée dans des requêtes SQL. 0,5 = la moitié du temps.",
			Targets:     []Target{{Expr: `sum by (db_system) (rate(traces_spanmetrics_latency_sum{` + sm + `, span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))`, Legend: "{{db_system}}"}}},
		Panel{Type: "timeseries", Title: "Requêtes SQL / s", Width: 8, Unit: "ops",
			Targets: []Target{{Expr: `sum by (db_system) (rate(traces_spanmetrics_calls_total{` + sm + `, span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))`, Legend: "{{db_system}}"}}},
		Panel{Type: "timeseries", Title: "Appels sortants (HTTP, Redis…) — p95", Width: 8, Unit: "s",
			Targets: []Target{{Expr: `topk(10, histogram_quantile(0.95, sum by (span_name, le) (rate(traces_spanmetrics_latency_bucket{` + sm + `, span_kind="SPAN_KIND_CLIENT"}[5m]))))`, Legend: "{{span_name}}"}}},
	)
	tq := `resource.observa.project="$project" && resource.observa.env="$env" && resource.service.name="$service"`
	d.Row("Traces")
	d.Add(
		Panel{Type: "traceql", Title: "Requêtes lentes (> 1 s)", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=server && duration > 1s}`}}},
		Panel{Type: "traceql", Title: "Requêtes en erreur", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=server && status=error}`}}},
	)
	logPanels(d)
	return d
}

// Container covers any service: resources, availability, logs.
func Container() *Dashboard {
	d := &Dashboard{
		UID: "observa-container", Title: "Service · conteneur", Folder: FolderPresets,
		Description: "Disponibilité, ressources et logs de n'importe quel service surveillé (preset generic et tous les autres).",
		Vars:        serviceVars("observa:service:last_seen_timestamp"),
		Links:       links(),
	}
	c := `project="$project", env="$env", service="$service"`
	d.Row("Disponibilité")
	d.Add(
		Panel{Type: "stat", Title: "Vu il y a", Width: 6, Height: 4, Unit: "s",
			Thresholds: []Threshold{{Color: "green"}, {Color: "red", Value: F(120)}},
			Targets:    []Target{{Expr: `time() - max(observa:service:last_seen_timestamp{` + c + `})`}}},
		Panel{Type: "stat", Title: "Conteneurs", Width: 6, Height: 4,
			Targets: []Target{{Expr: `count(count by (host, container) (container_last_seen{` + c + `}))`}}},
		Panel{Type: "stat", Title: "Redémarrages (24 h)", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(1)}, {Color: "red", Value: F(5)}},
			Targets:    []Target{{Expr: `sum(changes(container_start_time_seconds{` + c + `}[24h])) or vector(0)`}}},
		Panel{Type: "stat", Title: "Requêtes HTTP / s", Width: 6, Height: 4, Unit: "reqps",
			Description: "Seulement si le service est instrumenté (OpenTelemetry ou eBPF).",
			Targets:     []Target{{Expr: `observa:http_requests:rate5m{` + c + `}`}}},
	)
	d.Row("Ressources")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU (cœurs)", Width: 12,
			Targets: []Target{{Expr: `sum by (host, container) (rate(container_cpu_usage_seconds_total{` + c + `}[5m]))`, Legend: "{{host}} {{container}}"}}},
		Panel{Type: "timeseries", Title: "Mémoire", Width: 12, Unit: "bytes",
			Targets: []Target{
				{Expr: `sum by (host, container) (container_memory_working_set_bytes{` + c + `})`, Legend: "{{host}} {{container}}"},
				{Expr: `max by (host, container) (container_spec_memory_limit_bytes{` + c + `} > 0)`, Legend: "limite {{container}}"},
			}},
		Panel{Type: "timeseries", Title: "Réseau", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (container) (rate(container_network_receive_bytes_total{` + c + `}[5m]))`, Legend: "{{container}} reçu"},
				{Expr: `-sum by (container) (rate(container_network_transmit_bytes_total{` + c + `}[5m]))`, Legend: "{{container}} envoyé"},
			}},
		Panel{Type: "timeseries", Title: "Redémarrages", Width: 12, Bars: true,
			Targets: []Target{{Expr: `sum by (container) (changes(container_start_time_seconds{` + c + `}[5m]))`, Legend: "{{container}}"}}},
	)
	logPanels(d)
	return d
}

// Celery covers Celery workers and their Redis queue.
func Celery() *Dashboard {
	d := &Dashboard{
		UID: "observa-celery", Title: "Worker Celery", Folder: FolderPresets,
		Description: "Tâches (depuis les traces), échecs, durées et file d'attente Redis.",
		Vars: []Var{
			{Name: "project", Label: "Projet", Query: `label_values(container_last_seen{preset="celery"}, project)`},
			{Name: "env", Label: "Environnement", Query: `label_values(container_last_seen{preset="celery", project="$project"}, env)`},
			{Name: "service", Label: "Worker", Query: `label_values(container_last_seen{preset="celery", project="$project", env="$env"}, service)`},
		},
		Links: links(),
	}
	sm := pes + `, span_kind="SPAN_KIND_CONSUMER"`
	d.Row("Tâches")
	d.Add(
		Panel{Type: "stat", Title: "Tâches / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(traces_spanmetrics_calls_total{` + sm + `}[5m]))`}}},
		Panel{Type: "stat", Title: "Tâches en échec", Width: 6, Height: 4, Unit: "percentunit", Decimals: I(1),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.02)}, {Color: "red", Value: F(0.1)}},
			Targets:    []Target{{Expr: `observa:celery_task_failure_ratio:rate10m{` + pes + `}`}}},
		Panel{Type: "stat", Title: "File d'attente", Width: 6, Height: 4,
			Description: "Messages dans la liste Redis `celery` (preset redis appliqué au broker).",
			Thresholds:  []Threshold{{Color: "green"}, {Color: "orange", Value: F(50)}, {Color: "red", Value: F(100)}},
			Targets:     []Target{{Expr: `sum(redis_key_size{project="$project", env="$env", key="celery"}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Workers actifs", Width: 6, Height: 4,
			Targets: []Target{{Expr: `count((time() - observa:service:last_seen_timestamp{` + pes + `}) < 120) or vector(0)`}}},
		Panel{Type: "timeseries", Title: "Tâches / s par type", Width: 12, Unit: "ops", Stack: true,
			Targets: []Target{{Expr: `sum by (span_name) (rate(traces_spanmetrics_calls_total{` + sm + `}[5m]))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "Échecs / s par type", Width: 12, Unit: "ops",
			Targets: []Target{{Expr: `sum by (span_name) (rate(traces_spanmetrics_calls_total{` + sm + `, status_code="STATUS_CODE_ERROR"}[5m]))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "Durée p95 par type", Width: 12, Unit: "s",
			Targets: []Target{{Expr: `histogram_quantile(0.95, sum by (span_name, le) (rate(traces_spanmetrics_latency_bucket{` + sm + `}[5m])))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "File d'attente Redis", Width: 12,
			Targets: []Target{{Expr: `sum by (service) (redis_key_size{project="$project", env="$env", key="celery"})`, Legend: "{{service}}"}}},
	)
	tq := `resource.observa.project="$project" && resource.observa.env="$env" && resource.service.name="$service"`
	d.Row("Traces")
	d.Add(
		Panel{Type: "traceql", Title: "Tâches en échec", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=consumer && status=error}`}}},
		Panel{Type: "traceql", Title: "Tâches lentes (> 10 s)", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=consumer && duration > 10s}`}}},
	)
	logPanels(d)
	return d
}

// Postgres covers PostgreSQL through the agent's postgres exporter.
func Postgres() *Dashboard {
	d := &Dashboard{
		UID: "observa-postgres", Title: "PostgreSQL", Folder: FolderPresets,
		Description: "Connexions, transactions, cache, taille et verrous d'une base PostgreSQL.",
		Vars:        serviceVars("pg_up"),
		Links:       links(),
	}
	s := `{` + pes + `}`
	d.Row("État")
	d.Add(
		Panel{Type: "stat", Title: "Joignable", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "red"}, {Color: "green", Value: F(1)}},
			Targets:    []Target{{Expr: `max(pg_up` + s + `)`}}},
		Panel{Type: "stat", Title: "Connexions utilisées", Width: 6, Height: 4, Unit: "percentunit",
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.7)}, {Color: "red", Value: F(0.85)}},
			Targets:    []Target{{Expr: `sum(pg_stat_activity_count` + s + `) / max(pg_settings_max_connections` + s + `)`}}},
		Panel{Type: "stat", Title: "Transactions / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(pg_stat_database_xact_commit` + s + `[5m]) + rate(pg_stat_database_xact_rollback` + s + `[5m]))`}}},
		Panel{Type: "stat", Title: "Taille totale", Width: 6, Height: 4, Unit: "bytes",
			Targets: []Target{{Expr: `sum(pg_database_size_bytes` + s + `)`}}},
		Panel{Type: "timeseries", Title: "Connexions par état", Width: 12, Stack: true,
			Targets: []Target{
				{Expr: `sum by (state) (pg_stat_activity_count` + s + `)`, Legend: "{{state}}"},
				{Expr: `max(pg_settings_max_connections` + s + `)`, Legend: "max_connections"},
			}},
		Panel{Type: "timeseries", Title: "Transactions", Width: 12, Unit: "ops",
			Targets: []Target{
				{Expr: `sum(rate(pg_stat_database_xact_commit` + s + `[5m]))`, Legend: "commits"},
				{Expr: `sum(rate(pg_stat_database_xact_rollback` + s + `[5m]))`, Legend: "rollbacks"},
			}},
		Panel{Type: "timeseries", Title: "Taux de succès du cache", Width: 12, Unit: "percentunit", Max: F(1),
			Description: "Part des lectures servies depuis la mémoire. En dessous de 99 %, la base lit souvent le disque.",
			Targets:     []Target{{Expr: `sum(rate(pg_stat_database_blks_hit` + s + `[5m])) / (sum(rate(pg_stat_database_blks_hit` + s + `[5m])) + sum(rate(pg_stat_database_blks_read` + s + `[5m])))`, Legend: "cache"}}},
		Panel{Type: "timeseries", Title: "Lignes / s", Width: 12, Unit: "ops",
			Targets: []Target{
				{Expr: `sum(rate(pg_stat_database_tup_fetched` + s + `[5m]))`, Legend: "lues"},
				{Expr: `sum(rate(pg_stat_database_tup_inserted` + s + `[5m]))`, Legend: "insérées"},
				{Expr: `sum(rate(pg_stat_database_tup_updated` + s + `[5m]))`, Legend: "modifiées"},
				{Expr: `sum(rate(pg_stat_database_tup_deleted` + s + `[5m]))`, Legend: "supprimées"},
			}},
		Panel{Type: "timeseries", Title: "Plus longue transaction active", Width: 12, Unit: "s",
			Targets: []Target{{Expr: `max(pg_stat_activity_max_tx_duration` + s + `)`, Legend: "durée"}}},
		Panel{Type: "timeseries", Title: "Interblocages", Width: 12, Bars: true,
			Targets: []Target{{Expr: `sum(increase(pg_stat_database_deadlocks` + s + `[5m]))`, Legend: "deadlocks"}}},
		Panel{Type: "timeseries", Title: "Taille par base", Width: 24, Unit: "bytes",
			Targets: []Target{{Expr: `sum by (datname) (pg_database_size_bytes` + s + `)`, Legend: "{{datname}}"}}},
	)
	logPanels(d)
	return d
}

// Redis covers Redis/Valkey through the agent's redis exporter.
func Redis() *Dashboard {
	d := &Dashboard{
		UID: "observa-redis", Title: "Redis", Folder: FolderPresets,
		Description: "Mémoire, clients, commandes, efficacité du cache et file Celery.",
		Vars:        serviceVars("redis_up"),
		Links:       links(),
	}
	s := `{` + pes + `}`
	d.Row("État")
	d.Add(
		Panel{Type: "stat", Title: "Joignable", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "red"}, {Color: "green", Value: F(1)}},
			Targets:    []Target{{Expr: `max(redis_up` + s + `)`}}},
		Panel{Type: "stat", Title: "Mémoire utilisée", Width: 6, Height: 4, Unit: "bytes",
			Targets: []Target{{Expr: `max(redis_memory_used_bytes` + s + `)`}}},
		Panel{Type: "stat", Title: "Clients connectés", Width: 6, Height: 4,
			Targets: []Target{{Expr: `max(redis_connected_clients` + s + `)`}}},
		Panel{Type: "stat", Title: "Commandes / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(redis_commands_processed_total` + s + `[5m]))`}}},
		Panel{Type: "timeseries", Title: "Mémoire", Width: 12, Unit: "bytes",
			Targets: []Target{
				{Expr: `max(redis_memory_used_bytes` + s + `)`, Legend: "utilisée"},
				{Expr: `max(redis_memory_max_bytes` + s + ` > 0)`, Legend: "maxmemory"},
			}},
		Panel{Type: "timeseries", Title: "Taux de succès du cache", Width: 12, Unit: "percentunit", Max: F(1),
			Targets: []Target{{Expr: `sum(rate(redis_keyspace_hits_total` + s + `[5m])) / (sum(rate(redis_keyspace_hits_total` + s + `[5m])) + sum(rate(redis_keyspace_misses_total` + s + `[5m])))`, Legend: "hits"}}},
		Panel{Type: "timeseries", Title: "Clés par base", Width: 12,
			Targets: []Target{{Expr: `sum by (db) (redis_db_keys` + s + `)`, Legend: "{{db}}"}}},
		Panel{Type: "timeseries", Title: "Clés évincées / s", Width: 12, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(redis_evicted_keys_total` + s + `[5m]))`, Legend: "évincées"}}},
		Panel{Type: "timeseries", Title: "File Celery", Width: 24,
			Targets: []Target{{Expr: `sum(redis_key_size{` + pes + `, key="celery"})`, Legend: "messages en attente"}}},
	)
	logPanels(d)
	return d
}
