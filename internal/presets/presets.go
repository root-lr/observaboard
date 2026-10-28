// Package presets describes what ObservaBoard knows about each technology:
// how to recognise it, how to instrument it, which dashboard shows it and which
// alert thresholds a project may override in observa.yaml.
package presets

import (
	"fmt"
	"sort"
	"strings"
)

// Param is an alert threshold a project can override in observa.yaml
// (services.<name>.alerts.<Key>). The agent publishes it as the metric
// observa_service_threshold{threshold="<Metric>"} = value * Scale, and the
// central alert rules fall back to Default when no override exists.
type Param struct {
	Key         string  // key in observa.yaml, e.g. "latency_p95_ms"
	Metric      string  // threshold label value, e.g. "http_latency_p95_seconds"
	Default     float64 // in observa.yaml units
	Scale       float64 // multiply the observa.yaml value to get the metric value
	Min, Max    float64 // accepted range in observa.yaml units
	Description string
}

// MetricValue converts an observa.yaml value to the value stored in Prometheus.
func (p Param) MetricValue(v float64) float64 { return v * p.Scale }

// DefaultMetricValue is the fallback used by alert rules.
func (p Param) DefaultMetricValue() float64 { return p.Default * p.Scale }

// Exporter kinds run by the agent for data stores.
const (
	ExporterNone     = ""
	ExporterPostgres = "postgres"
	ExporterRedis    = "redis"
)

// Instrumentation modes.
const (
	InstrOTel = "otel" // OpenTelemetry zero-code (level 2)
	InstrEBPF = "ebpf" // HTTP metrics + basic traces via eBPF (level 1, optional)
	InstrNone = "none" // logs + container metrics only (level 1)
)

// Runtimes supported for zero-code instrumentation in V1.
const (
	RuntimePython = "python"
	RuntimeNode   = "node"
)

// Preset is a bundle of knowledge about one technology.
type Preset struct {
	Name        string
	Title       string
	Description string
	Dashboard   string   // Grafana dashboard uid
	Alerts      []string // alert names defined for this preset (see rules package)
	Params      []Param
	Exporter    string
	DefaultPort int
	// Runtime is the runtime required for zero-code instrumentation ("" = any).
	Runtime string
	// HTTP is true when the service serves HTTP and gets the HTTP alerts.
	HTTP bool
}

var httpParams = []Param{
	{Key: "latency_p95_ms", Metric: "http_latency_p95_seconds", Default: 1000, Scale: 0.001, Min: 10, Max: 600000,
		Description: "Maximum p95 latency before alerting (milliseconds)"},
	{Key: "error_rate_pct", Metric: "http_error_ratio", Default: 5, Scale: 0.01, Min: 0.01, Max: 100,
		Description: "Maximum share of 5xx responses before alerting (percent)"},
}

var all = []Preset{
	{
		Name: "django", Title: "Django", HTTP: true, Runtime: RuntimePython, DefaultPort: 8000,
		Description: "Django application (gunicorn, uvicorn or manage.py). Traces of views, SQL queries and outgoing calls.",
		Dashboard:   "observa-http",
		Alerts:      []string{"ObservaHighErrorRate", "ObservaHighLatency", "ObservaServiceDown"},
		Params:      httpParams,
	},
	{
		Name: "fastapi", Title: "FastAPI", HTTP: true, Runtime: RuntimePython, DefaultPort: 8000,
		Description: "FastAPI / Starlette application (uvicorn, gunicorn). Traces of routes and outgoing calls.",
		Dashboard:   "observa-http",
		Alerts:      []string{"ObservaHighErrorRate", "ObservaHighLatency", "ObservaServiceDown"},
		Params:      httpParams,
	},
	{
		Name: "celery", Title: "Celery", Runtime: RuntimePython,
		Description: "Celery worker. Task duration and failures (traces), queue via the redis preset.",
		Dashboard:   "observa-celery",
		Alerts:      []string{"ObservaCeleryTaskFailures", "ObservaServiceDown"},
		Params: []Param{
			{Key: "task_failure_pct", Metric: "celery_task_failure_ratio", Default: 10, Scale: 0.01, Min: 0.01, Max: 100,
				Description: "Maximum share of failed tasks before alerting (percent)"},
		},
	},
	{
		Name: "postgres", Title: "PostgreSQL", Exporter: ExporterPostgres, DefaultPort: 5432,
		Description: "PostgreSQL database. Connections, transactions, cache, size, locks.",
		Dashboard:   "observa-postgres",
		Alerts:      []string{"ObservaPostgresDown", "ObservaPostgresConnectionsHigh", "ObservaServiceDown"},
		Params: []Param{
			{Key: "connections_pct", Metric: "postgres_connections_ratio", Default: 80, Scale: 0.01, Min: 1, Max: 100,
				Description: "Maximum share of max_connections in use before alerting (percent)"},
		},
	},
	{
		Name: "redis", Title: "Redis", Exporter: ExporterRedis, DefaultPort: 6379,
		Description: "Redis or Valkey. Memory, clients, commands, Celery queue length.",
		Dashboard:   "observa-redis",
		Alerts:      []string{"ObservaRedisDown", "ObservaRedisMemoryHigh", "ObservaQueueBacklog", "ObservaServiceDown"},
		Params: []Param{
			{Key: "memory_pct", Metric: "redis_memory_ratio", Default: 90, Scale: 0.01, Min: 1, Max: 100,
				Description: "Maximum share of maxmemory in use before alerting (percent)"},
			{Key: "queue_length", Metric: "queue_length", Default: 100, Scale: 1, Min: 1, Max: 100000000,
				Description: "Maximum number of messages waiting in the `celery` queue before alerting"},
		},
	},
	{
		Name: "generic", Title: "Generic", HTTP: true,
		Description: "Any other container: logs, resources, availability. HTTP metrics if the app is instrumented (Node.js, eBPF).",
		Dashboard:   "observa-container",
		Alerts:      []string{"ObservaServiceDown", "ObservaContainerRestarting", "ObservaHighErrorRate", "ObservaHighLatency"},
		Params:      httpParams,
	},
}

// All returns every preset, sorted by name.
func All() []Preset {
	out := append([]Preset(nil), all...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the preset with the given name.
func Get(name string) (Preset, bool) {
	for _, p := range all {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// Names returns the preset names, sorted.
func Names() []string {
	var n []string
	for _, p := range All() {
		n = append(n, p.Name)
	}
	return n
}

// Param returns the named threshold parameter of a preset.
func (p Preset) Param(key string) (Param, bool) {
	for _, q := range p.Params {
		if q.Key == key {
			return q, true
		}
	}
	return Param{}, false
}

// ParamKeys lists the threshold keys a preset accepts.
func (p Preset) ParamKeys() []string {
	var k []string
	for _, q := range p.Params {
		k = append(k, q.Key)
	}
	return k
}

// DefaultInstrumentation picks the instrumentation for a service of this preset
// running on the given runtime.
func (p Preset) DefaultInstrumentation(runtime string) string {
	if p.Exporter != ExporterNone {
		return InstrNone
	}
	if runtime == RuntimePython || runtime == RuntimeNode {
		if p.Runtime == "" || p.Runtime == runtime {
			return InstrOTel
		}
	}
	return InstrNone
}

// AllThresholdParams returns every distinct threshold parameter across presets,
// keyed by metric name. Used to generate the fallback rules.
func AllThresholdParams() []Param {
	seen := map[string]Param{}
	for _, p := range all {
		for _, q := range p.Params {
			if prev, ok := seen[q.Metric]; ok && prev.DefaultMetricValue() != q.DefaultMetricValue() {
				panic(fmt.Sprintf("threshold %s has two different defaults", q.Metric))
			}
			seen[q.Metric] = q
		}
	}
	var out []Param
	for _, q := range seen {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metric < out[j].Metric })
	return out
}

// Describe returns a human-readable description used by `observa preset show`.
func (p Preset) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n  %s\n", p.Name, p.Title, p.Description)
	fmt.Fprintf(&b, "  Dashboard: %s\n", p.Dashboard)
	if p.Runtime != "" {
		fmt.Fprintf(&b, "  Zero-code instrumentation: %s (OpenTelemetry)\n", p.Runtime)
	}
	if p.Exporter != "" {
		fmt.Fprintf(&b, "  Exporter run by the agent: %s\n", p.Exporter)
	}
	fmt.Fprintf(&b, "  Alerts: %s\n", strings.Join(p.Alerts, ", "))
	if len(p.Params) > 0 {
		b.WriteString("  Overridable thresholds (services.<name>.alerts in observa.yaml):\n")
		for _, q := range p.Params {
			fmt.Fprintf(&b, "    %-18s default %-8g %s\n", q.Key, q.Default, q.Description)
		}
	}
	return b.String()
}
