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
	varProjectMulti = Var{Name: "project", Label: "Project", Multi: true, Query: `label_values(observa:service:present, project)`}
	varEnvMulti     = Var{Name: "env", Label: "Environment", Multi: true, Query: `label_values(observa:service:present{project=~"$project"}, env)`}
)

const pe = `project=~"$project", env=~"$env"`                    // multi-select scope
const pes = `project="$project", env="$env", service="$service"` // single service scope

func links() []Link {
	return []Link{
		{Title: "Overview", URL: "/d/observa-overview"},
		{Title: "Servers", URL: "/d/observa-host"},
		{Title: "HTTP services", URL: "/d/observa-http"},
		{Title: "Containers", URL: "/d/observa-container"},
		{Title: "Runbooks", URL: "/runbooks/"},
	}
}

// Overview is the home dashboard.
func Overview() *Dashboard {
	d := &Dashboard{
		UID: "observa-overview", Title: "ObservaBoard · Overview", Folder: FolderMain,
		Description: "Status of all monitored projects, services and servers.",
		Vars:        []Var{varProjectMulti, varEnvMulti},
		Links:       links(),
	}
	d.Row("General status")
	d.Add(
		Panel{Type: "stat", Title: "Connected servers", Width: 4, Height: 4,
			Description: "Agents that sent data in the last 5 minutes.",
			Targets:     []Target{{Expr: `count(count by (host) (node_boot_time_seconds{job="observa/host"})) or vector(0)`}}},
		Panel{Type: "stat", Title: "Monitored services", Width: 4, Height: 4,
			Targets: []Target{{Expr: `count(observa:service:present{` + pe + `}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Stopped services", Width: 4, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "red", Value: F(1)}},
			Targets:    []Target{{Expr: `count((time() - observa:service:last_seen_timestamp{` + pe + `}) > 120) or vector(0)`}}},
		Panel{Type: "stat", Title: "Firing alerts", Width: 4, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(1)}},
			Targets:    []Target{{Expr: `count(ALERTS{alertstate="firing", severity!="none"}) or vector(0)`}}},
		Panel{Type: "stat", Title: "HTTP requests / s", Width: 4, Height: 4, Unit: "reqps",
			Targets: []Target{{Expr: `sum(observa:http_requests:rate5m{` + pe + `}) or vector(0)`}}},
		Panel{Type: "stat", Title: "5xx responses", Width: 4, Height: 4, Unit: "percentunit", Decimals: I(2),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.01)}, {Color: "red", Value: F(0.05)}},
			Targets:    []Target{{Expr: `(sum(observa:http_errors:rate5m{` + pe + `}) or vector(0)) / sum(observa:http_requests:rate5m{` + pe + `})`}}},
	)
	d.Row("Services")
	d.Add(
		Panel{Type: "table", Title: "Services", Width: 24, Height: 9,
			Description: "Traffic, errors and latency per service (last 5 minutes). Click a service to open its dashboard.",
			Targets: []Target{
				{Expr: `max by (project, env, service) (time() - observa:service:last_seen_timestamp{` + pe + `})`, Instant: true, Table: true},
				{Expr: `observa:http_requests:rate5m{` + pe + `}`, Instant: true, Table: true},
				{Expr: `observa:http_error_ratio:rate5m{` + pe + `}`, Instant: true, Table: true},
				{Expr: `observa:http_latency_p95:5m{` + pe + `}`, Instant: true, Table: true},
			},
			Rename: map[string]string{
				"project": "Project", "env": "Env", "service": "Service",
				"Value #A": "Last seen (s ago)", "Value #B": "Req/s", "Value #C": "5xx", "Value #D": "p95 (s)",
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
		Panel{Type: "timeseries", Title: "Share of 5xx responses per service", Width: 12, Unit: "percentunit", Min: F(0),
			Targets: []Target{{Expr: `observa:http_error_ratio:rate5m{` + pe + `}`, Legend: "{{project}}/{{env}}/{{service}}"}}},
		Panel{Type: "timeseries", Title: "p95 latency per service", Width: 12, Unit: "s", Min: F(0),
			Targets: []Target{{Expr: `observa:http_latency_p95:5m{` + pe + `}`, Legend: "{{project}}/{{env}}/{{service}}"}}},
		Panel{Type: "table", Title: "Firing alerts", Width: 24, Height: 7,
			Targets: []Target{{Expr: `ALERTS{alertstate="firing", severity!="none"}`, Instant: true, Table: true}},
			Hide:    []string{"Value", "__name__", "alertstate", "notify_critical", "notify_warning"},
			Rename:  map[string]string{"alertname": "Alert", "severity": "Severity", "project": "Project", "env": "Env", "service": "Service", "host": "Server"},
		},
	)
	d.Row("Servers")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU per server", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - avg by (host) (rate(node_cpu_seconds_total{job="observa/host", mode="idle"}[5m]))`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Memory per server", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
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
		UID: "observa-host", Title: "ObservaBoard · Servers", Folder: FolderMain,
		Description: "Resources of servers running an agent, and the heaviest containers.",
		Vars:        []Var{{Name: "host", Label: "Server", Multi: true, Query: `label_values(node_boot_time_seconds{job="observa/host"}, host)`}},
		Links:       links(),
	}
	h := `job="observa/host", host=~"$host"`
	fs := h + `, fstype!~"tmpfs|overlay|squashfs|nsfs|ramfs|fuse.*", mountpoint!~"/(run|var/lib/docker|snap)($|/.*)"`
	d.Row("Resources")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU used", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - avg by (host) (rate(node_cpu_seconds_total{` + h + `, mode="idle"}[5m]))`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Load (5 min) per core", Width: 12, Min: F(0),
			Targets: []Target{{Expr: `node_load5{` + h + `} / on (host) count by (host) (node_cpu_seconds_total{` + h + `, mode="idle"})`, Legend: "{{host}}"}}},
		Panel{Type: "timeseries", Title: "Memory used", Width: 12, Unit: "bytes", Min: F(0),
			Targets: []Target{
				{Expr: `node_memory_MemTotal_bytes{` + h + `} - node_memory_MemAvailable_bytes{` + h + `}`, Legend: "{{host}} used"},
				{Expr: `node_memory_MemTotal_bytes{` + h + `}`, Legend: "{{host}} total"},
			}},
		Panel{Type: "timeseries", Title: "Disk usage", Width: 12, Unit: "percentunit", Min: F(0), Max: F(1),
			Targets: []Target{{Expr: `1 - node_filesystem_avail_bytes{` + fs + `} / node_filesystem_size_bytes{` + fs + `}`, Legend: "{{host}} {{mountpoint}}"}}},
		Panel{Type: "timeseries", Title: "Network", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (host) (rate(node_network_receive_bytes_total{` + h + `, device!~"lo|veth.*|br-.*|docker.*"}[5m]))`, Legend: "{{host}} received"},
				{Expr: `-sum by (host) (rate(node_network_transmit_bytes_total{` + h + `, device!~"lo|veth.*|br-.*|docker.*"}[5m]))`, Legend: "{{host}} sent"},
			}},
		Panel{Type: "timeseries", Title: "Disk throughput", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (host) (rate(node_disk_read_bytes_total{` + h + `}[5m]))`, Legend: "{{host}} read"},
				{Expr: `-sum by (host) (rate(node_disk_written_bytes_total{` + h + `}[5m]))`, Legend: "{{host}} write"},
			}},
	)
	d.Row("Containers")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU of the 10 busiest containers", Width: 12, Unit: "short",
			Description: "In cores used.",
			Targets:     []Target{{Expr: `topk(10, sum by (host, container) (rate(container_cpu_usage_seconds_total{host=~"$host", container!=""}[5m])))`, Legend: "{{host}} {{container}}"}}},
		Panel{Type: "timeseries", Title: "Memory of the 10 heaviest containers", Width: 12, Unit: "bytes",
			Targets: []Target{{Expr: `topk(10, sum by (host, container) (container_memory_working_set_bytes{host=~"$host", container!=""}))`, Legend: "{{host}} {{container}}"}}},
	)
	return d
}

func serviceVars(metric string) []Var {
	return []Var{
		{Name: "project", Label: "Project", Query: fmt.Sprintf(`label_values(%s, project)`, metric)},
		{Name: "env", Label: "Environment", Query: fmt.Sprintf(`label_values(%s{project="$project"}, env)`, metric)},
		{Name: "service", Label: "Service", Query: fmt.Sprintf(`label_values(%s{project="$project", env="$env"}, service)`, metric)},
	}
}

const logsSel = `{project="$project", env="$env", service="$service"}`
const errorFilter = ` |~ "(?i)(error|exception|traceback|fatal|critical)"`

func logPanels(d *Dashboard) {
	d.Row("Logs")
	d.Add(
		Panel{Type: "timeseries", Loki: true, Title: "Log volume", Width: 12, Height: 6, Bars: true, Stack: true,
			Targets: []Target{{Expr: `sum(count_over_time(` + logsSel + ` [1m]))`, Legend: "all lines"}},
		},
		Panel{Type: "timeseries", Loki: true, Title: "Error lines", Width: 12, Height: 6, Bars: true,
			Targets: []Target{{Expr: `sum(count_over_time(` + logsSel + errorFilter + ` [1m]))`, Legend: "errors"}},
		},
		Panel{Type: "logs", Title: "Service logs", Width: 24, Height: 12,
			Description: "Click a line containing trace_id to open the matching trace.",
			Targets:     []Target{{Expr: logsSel}}},
	)
}

// HTTP covers Django, FastAPI and any service exposing OpenTelemetry HTTP metrics.
func HTTP() *Dashboard {
	d := &Dashboard{
		UID: "observa-http", Title: "HTTP service (Django, FastAPI…)", Folder: FolderPresets,
		Description: "Traffic, errors, latency, routes, dependencies, traces and logs of an instrumented HTTP service. Metrics computed from traces (Tempo span metrics).",
		Vars:        serviceVars("observa:http_requests:rate5m"),
		Links:       links(),
	}
	sel := `{` + pes + `}`
	srv := `{` + pes + `, span_kind="SPAN_KIND_SERVER"}`
	srv5xx := `{` + pes + `, span_kind="SPAN_KIND_SERVER", http_response_status_code=~"5.."}`
	calls := `traces_spanmetrics_calls_total`
	lat := `traces_spanmetrics_latency_bucket`
	d.Row("Traffic")
	d.Add(
		Panel{Type: "stat", Title: "Requests / s", Width: 6, Height: 4, Unit: "reqps",
			Targets: []Target{{Expr: `observa:http_requests:rate5m` + sel}}},
		Panel{Type: "stat", Title: "5xx responses", Width: 6, Height: 4, Unit: "percentunit", Decimals: I(2),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.01)}, {Color: "red", Value: F(0.05)}},
			Targets:    []Target{{Expr: `observa:http_error_ratio:rate5m` + sel}}},
		Panel{Type: "stat", Title: "p95 latency", Width: 6, Height: 4, Unit: "s",
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.5)}, {Color: "red", Value: F(1)}},
			Targets:    []Target{{Expr: `observa:http_latency_p95:5m` + sel}}},
		Panel{Type: "stat", Title: "p99 latency", Width: 6, Height: 4, Unit: "s",
			Targets: []Target{{Expr: `histogram_quantile(0.99, sum by (le) (rate(` + lat + srv + `[5m])))`}}},
		Panel{Type: "timeseries", Title: "Requests / s by status class", Width: 12, Unit: "reqps", Stack: true,
			Targets: []Target{{Expr: `sum by (code) (label_replace(rate(` + calls + srv + `[5m]), "code", "${1}xx", "http_response_status_code", "([0-9]).*"))`, Legend: "{{code}}"}}},
		Panel{Type: "timeseries", Title: "Latency", Width: 12, Unit: "s", Min: F(0),
			Targets: []Target{
				{Expr: `histogram_quantile(0.50, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p50"},
				{Expr: `histogram_quantile(0.95, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p95"},
				{Expr: `histogram_quantile(0.99, sum by (le) (rate(` + lat + srv + `[5m])))`, Legend: "p99"},
				{Expr: `observa:threshold:http_latency_p95_seconds` + sel, Legend: "p95 alert threshold"},
			}},
		Panel{Type: "timeseries", Title: "Share of 5xx responses and threshold", Width: 12, Unit: "percentunit", Min: F(0),
			Targets: []Target{
				{Expr: `observa:http_error_ratio:rate5m` + sel, Legend: "5xx"},
				{Expr: `observa:threshold:http_error_ratio` + sel, Legend: "alert threshold"},
			}},
		Panel{Type: "timeseries", Title: "Failed requests per route", Width: 12, Unit: "reqps",
			Targets: []Target{{Expr: `topk(10, sum by (http_request_method, http_route) (rate(` + calls + srv5xx + `[5m])))`, Legend: "{{http_request_method}} {{http_route}}"}}},
	)
	d.Row("Routes")
	d.Add(
		Panel{Type: "table", Title: "Routes (last 5 minutes)", Width: 24, Height: 9,
			Targets: []Target{
				{Expr: `sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m]))`, Instant: true, Table: true},
				{Expr: `histogram_quantile(0.95, sum by (http_request_method, http_route, le) (rate(` + lat + srv + `[5m])))`, Instant: true, Table: true},
				{Expr: `(sum by (http_request_method, http_route) (rate(` + calls + srv5xx + `[5m])) or sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m])) * 0) / sum by (http_request_method, http_route) (rate(` + calls + srv + `[5m]))`, Instant: true, Table: true},
			},
			Rename: map[string]string{"http_request_method": "Method", "http_route": "Route", "Value #A": "Req/s", "Value #B": "p95 (s)", "Value #C": "5xx"},
			Overrides: []map[string]any{
				fieldUnit("Value #A", "reqps"), fieldUnit("Value #B", "s"), fieldUnit("Value #C", "percentunit"),
			},
		},
		Panel{Type: "timeseries", Title: "Slowest routes (p95)", Width: 24, Unit: "s",
			Targets: []Target{{Expr: `topk(10, histogram_quantile(0.95, sum by (http_request_method, http_route, le) (rate(` + lat + srv + `[5m]))))`, Legend: "{{http_request_method}} {{http_route}}"}}},
	)
	sm := pes
	d.Row("Dependencies (computed from traces)")
	d.Add(
		Panel{Type: "timeseries", Title: "Time spent in the database", Width: 8, Unit: "percentunit", Min: F(0),
			Description: "Share of time (in seconds per second) spent in SQL queries. 0.5 = half the time.",
			Targets:     []Target{{Expr: `sum by (db_system) (rate(traces_spanmetrics_latency_sum{` + sm + `, span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))`, Legend: "{{db_system}}"}}},
		Panel{Type: "timeseries", Title: "SQL queries / s", Width: 8, Unit: "ops",
			Targets: []Target{{Expr: `sum by (db_system) (rate(traces_spanmetrics_calls_total{` + sm + `, span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))`, Legend: "{{db_system}}"}}},
		Panel{Type: "timeseries", Title: "Outgoing calls (HTTP, Redis…) — p95", Width: 8, Unit: "s",
			Targets: []Target{{Expr: `topk(10, histogram_quantile(0.95, sum by (span_name, le) (rate(traces_spanmetrics_latency_bucket{` + sm + `, span_kind="SPAN_KIND_CLIENT"}[5m]))))`, Legend: "{{span_name}}"}}},
	)
	tq := `resource.observa.project="$project" && resource.observa.env="$env" && resource.service.name="$service"`
	d.Row("Traces")
	d.Add(
		Panel{Type: "traceql", Title: "Slow requests (> 1 s)", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=server && duration > 1s}`}}},
		Panel{Type: "traceql", Title: "Failed requests", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=server && status=error}`}}},
	)
	logPanels(d)
	return d
}

// Container covers any service: resources, availability, logs.
func Container() *Dashboard {
	d := &Dashboard{
		UID: "observa-container", Title: "Service · container", Folder: FolderPresets,
		Description: "Availability, resources and logs of any monitored service (generic preset and all others).",
		Vars:        serviceVars("observa:service:last_seen_timestamp"),
		Links:       links(),
	}
	c := `project="$project", env="$env", service="$service"`
	d.Row("Availability")
	d.Add(
		Panel{Type: "stat", Title: "Last seen", Width: 6, Height: 4, Unit: "s",
			Thresholds: []Threshold{{Color: "green"}, {Color: "red", Value: F(120)}},
			Targets:    []Target{{Expr: `time() - max(observa:service:last_seen_timestamp{` + c + `})`}}},
		Panel{Type: "stat", Title: "Containers", Width: 6, Height: 4,
			Targets: []Target{{Expr: `count(count by (host, container) (container_last_seen{` + c + `}))`}}},
		Panel{Type: "stat", Title: "Restarts (24 h)", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(1)}, {Color: "red", Value: F(5)}},
			Targets:    []Target{{Expr: `sum(changes(container_start_time_seconds{` + c + `}[24h])) or vector(0)`}}},
		Panel{Type: "stat", Title: "HTTP requests / s", Width: 6, Height: 4, Unit: "reqps",
			Description: "Only if the service is instrumented (OpenTelemetry or eBPF).",
			Targets:     []Target{{Expr: `observa:http_requests:rate5m{` + c + `}`}}},
	)
	d.Row("Resources")
	d.Add(
		Panel{Type: "timeseries", Title: "CPU (cores)", Width: 12,
			Targets: []Target{{Expr: `sum by (host, container) (rate(container_cpu_usage_seconds_total{` + c + `}[5m]))`, Legend: "{{host}} {{container}}"}}},
		Panel{Type: "timeseries", Title: "Memory", Width: 12, Unit: "bytes",
			Targets: []Target{
				{Expr: `sum by (host, container) (container_memory_working_set_bytes{` + c + `})`, Legend: "{{host}} {{container}}"},
				{Expr: `max by (host, container) (container_spec_memory_limit_bytes{` + c + `} > 0)`, Legend: "limit {{container}}"},
			}},
		Panel{Type: "timeseries", Title: "Network", Width: 12, Unit: "Bps",
			Targets: []Target{
				{Expr: `sum by (container) (rate(container_network_receive_bytes_total{` + c + `}[5m]))`, Legend: "{{container}} received"},
				{Expr: `-sum by (container) (rate(container_network_transmit_bytes_total{` + c + `}[5m]))`, Legend: "{{container}} sent"},
			}},
		Panel{Type: "timeseries", Title: "Restarts", Width: 12, Bars: true,
			Targets: []Target{{Expr: `sum by (container) (changes(container_start_time_seconds{` + c + `}[5m]))`, Legend: "{{container}}"}}},
	)
	logPanels(d)
	return d
}

// Celery covers Celery workers and their Redis queue.
func Celery() *Dashboard {
	d := &Dashboard{
		UID: "observa-celery", Title: "Celery worker", Folder: FolderPresets,
		Description: "Tasks (from traces), failures, durations and Redis queue.",
		Vars: []Var{
			{Name: "project", Label: "Project", Query: `label_values(container_last_seen{preset="celery"}, project)`},
			{Name: "env", Label: "Environment", Query: `label_values(container_last_seen{preset="celery", project="$project"}, env)`},
			{Name: "service", Label: "Worker", Query: `label_values(container_last_seen{preset="celery", project="$project", env="$env"}, service)`},
		},
		Links: links(),
	}
	sm := pes + `, span_kind="SPAN_KIND_CONSUMER"`
	d.Row("Tasks")
	d.Add(
		Panel{Type: "stat", Title: "Tasks / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(traces_spanmetrics_calls_total{` + sm + `}[5m]))`}}},
		Panel{Type: "stat", Title: "Failed tasks", Width: 6, Height: 4, Unit: "percentunit", Decimals: I(1),
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.02)}, {Color: "red", Value: F(0.1)}},
			Targets:    []Target{{Expr: `observa:celery_task_failure_ratio:rate10m{` + pes + `}`}}},
		Panel{Type: "stat", Title: "Queue", Width: 6, Height: 4,
			Description: "Messages in the Redis `celery` list (redis preset applied to the broker).",
			Thresholds:  []Threshold{{Color: "green"}, {Color: "orange", Value: F(50)}, {Color: "red", Value: F(100)}},
			Targets:     []Target{{Expr: `sum(redis_key_size{project="$project", env="$env", key="celery"}) or vector(0)`}}},
		Panel{Type: "stat", Title: "Active workers", Width: 6, Height: 4,
			Targets: []Target{{Expr: `count((time() - observa:service:last_seen_timestamp{` + pes + `}) < 120) or vector(0)`}}},
		Panel{Type: "timeseries", Title: "Tasks / s by type", Width: 12, Unit: "ops", Stack: true,
			Targets: []Target{{Expr: `sum by (span_name) (rate(traces_spanmetrics_calls_total{` + sm + `}[5m]))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "Failures / s by type", Width: 12, Unit: "ops",
			Targets: []Target{{Expr: `sum by (span_name) (rate(traces_spanmetrics_calls_total{` + sm + `, status_code="STATUS_CODE_ERROR"}[5m]))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "p95 duration by type", Width: 12, Unit: "s",
			Targets: []Target{{Expr: `histogram_quantile(0.95, sum by (span_name, le) (rate(traces_spanmetrics_latency_bucket{` + sm + `}[5m])))`, Legend: "{{span_name}}"}}},
		Panel{Type: "timeseries", Title: "Redis queue", Width: 12,
			Targets: []Target{{Expr: `sum by (service) (redis_key_size{project="$project", env="$env", key="celery"})`, Legend: "{{service}}"}}},
	)
	tq := `resource.observa.project="$project" && resource.observa.env="$env" && resource.service.name="$service"`
	d.Row("Traces")
	d.Add(
		Panel{Type: "traceql", Title: "Failed tasks", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=consumer && status=error}`}}},
		Panel{Type: "traceql", Title: "Slow tasks (> 10 s)", Width: 12, Height: 9,
			Targets: []Target{{Expr: `{` + tq + ` && kind=consumer && duration > 10s}`}}},
	)
	logPanels(d)
	return d
}

// Postgres covers PostgreSQL through the agent's postgres exporter.
func Postgres() *Dashboard {
	d := &Dashboard{
		UID: "observa-postgres", Title: "PostgreSQL", Folder: FolderPresets,
		Description: "Connections, transactions, cache, size and locks of a PostgreSQL database.",
		Vars:        serviceVars("pg_up"),
		Links:       links(),
	}
	s := `{` + pes + `}`
	d.Row("Status")
	d.Add(
		Panel{Type: "stat", Title: "Reachable", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "red"}, {Color: "green", Value: F(1)}},
			Targets:    []Target{{Expr: `max(pg_up` + s + `)`}}},
		Panel{Type: "stat", Title: "Connections used", Width: 6, Height: 4, Unit: "percentunit",
			Thresholds: []Threshold{{Color: "green"}, {Color: "orange", Value: F(0.7)}, {Color: "red", Value: F(0.85)}},
			Targets:    []Target{{Expr: `sum(pg_stat_activity_count` + s + `) / max(pg_settings_max_connections` + s + `)`}}},
		Panel{Type: "stat", Title: "Transactions / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(pg_stat_database_xact_commit` + s + `[5m]) + rate(pg_stat_database_xact_rollback` + s + `[5m]))`}}},
		Panel{Type: "stat", Title: "Total size", Width: 6, Height: 4, Unit: "bytes",
			Targets: []Target{{Expr: `sum(pg_database_size_bytes` + s + `)`}}},
		Panel{Type: "timeseries", Title: "Connections by state", Width: 12, Stack: true,
			Targets: []Target{
				{Expr: `sum by (state) (pg_stat_activity_count` + s + `)`, Legend: "{{state}}"},
				{Expr: `max(pg_settings_max_connections` + s + `)`, Legend: "max_connections"},
			}},
		Panel{Type: "timeseries", Title: "Transactions", Width: 12, Unit: "ops",
			Targets: []Target{
				{Expr: `sum(rate(pg_stat_database_xact_commit` + s + `[5m]))`, Legend: "commits"},
				{Expr: `sum(rate(pg_stat_database_xact_rollback` + s + `[5m]))`, Legend: "rollbacks"},
			}},
		Panel{Type: "timeseries", Title: "Cache hit ratio", Width: 12, Unit: "percentunit", Max: F(1),
			Description: "Share of reads served from memory. Below 99%, the database often reads from disk.",
			Targets:     []Target{{Expr: `sum(rate(pg_stat_database_blks_hit` + s + `[5m])) / (sum(rate(pg_stat_database_blks_hit` + s + `[5m])) + sum(rate(pg_stat_database_blks_read` + s + `[5m])))`, Legend: "cache"}}},
		Panel{Type: "timeseries", Title: "Rows / s", Width: 12, Unit: "ops",
			Targets: []Target{
				{Expr: `sum(rate(pg_stat_database_tup_fetched` + s + `[5m]))`, Legend: "fetched"},
				{Expr: `sum(rate(pg_stat_database_tup_inserted` + s + `[5m]))`, Legend: "inserted"},
				{Expr: `sum(rate(pg_stat_database_tup_updated` + s + `[5m]))`, Legend: "updated"},
				{Expr: `sum(rate(pg_stat_database_tup_deleted` + s + `[5m]))`, Legend: "deleted"},
			}},
		Panel{Type: "timeseries", Title: "Longest active transaction", Width: 12, Unit: "s",
			Targets: []Target{{Expr: `max(pg_stat_activity_max_tx_duration` + s + `)`, Legend: "duration"}}},
		Panel{Type: "timeseries", Title: "Deadlocks", Width: 12, Bars: true,
			Targets: []Target{{Expr: `sum(increase(pg_stat_database_deadlocks` + s + `[5m]))`, Legend: "deadlocks"}}},
		Panel{Type: "timeseries", Title: "Size per database", Width: 24, Unit: "bytes",
			Targets: []Target{{Expr: `sum by (datname) (pg_database_size_bytes` + s + `)`, Legend: "{{datname}}"}}},
	)
	logPanels(d)
	return d
}

// Redis covers Redis/Valkey through the agent's redis exporter.
func Redis() *Dashboard {
	d := &Dashboard{
		UID: "observa-redis", Title: "Redis", Folder: FolderPresets,
		Description: "Memory, clients, commands, cache efficiency and Celery queue.",
		Vars:        serviceVars("redis_up"),
		Links:       links(),
	}
	s := `{` + pes + `}`
	d.Row("Status")
	d.Add(
		Panel{Type: "stat", Title: "Reachable", Width: 6, Height: 4,
			Thresholds: []Threshold{{Color: "red"}, {Color: "green", Value: F(1)}},
			Targets:    []Target{{Expr: `max(redis_up` + s + `)`}}},
		Panel{Type: "stat", Title: "Memory used", Width: 6, Height: 4, Unit: "bytes",
			Targets: []Target{{Expr: `max(redis_memory_used_bytes` + s + `)`}}},
		Panel{Type: "stat", Title: "Connected clients", Width: 6, Height: 4,
			Targets: []Target{{Expr: `max(redis_connected_clients` + s + `)`}}},
		Panel{Type: "stat", Title: "Commands / s", Width: 6, Height: 4, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(redis_commands_processed_total` + s + `[5m]))`}}},
		Panel{Type: "timeseries", Title: "Memory", Width: 12, Unit: "bytes",
			Targets: []Target{
				{Expr: `max(redis_memory_used_bytes` + s + `)`, Legend: "used"},
				{Expr: `max(redis_memory_max_bytes` + s + ` > 0)`, Legend: "maxmemory"},
			}},
		Panel{Type: "timeseries", Title: "Cache hit ratio", Width: 12, Unit: "percentunit", Max: F(1),
			Targets: []Target{{Expr: `sum(rate(redis_keyspace_hits_total` + s + `[5m])) / (sum(rate(redis_keyspace_hits_total` + s + `[5m])) + sum(rate(redis_keyspace_misses_total` + s + `[5m])))`, Legend: "hits"}}},
		Panel{Type: "timeseries", Title: "Keys per database", Width: 12,
			Targets: []Target{{Expr: `sum by (db) (redis_db_keys` + s + `)`, Legend: "{{db}}"}}},
		Panel{Type: "timeseries", Title: "Evicted keys / s", Width: 12, Unit: "ops",
			Targets: []Target{{Expr: `sum(rate(redis_evicted_keys_total` + s + `[5m]))`, Legend: "evicted"}}},
		Panel{Type: "timeseries", Title: "Celery queue", Width: 24,
			Targets: []Target{{Expr: `sum(redis_key_size{` + pes + `, key="celery"})`, Legend: "pending messages"}}},
	)
	logPanels(d)
	return d
}
