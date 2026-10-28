// Package version holds the CLI version and the pinned image versions of every
// component ObservaBoard deploys. Bumping a component means changing it here,
// then running the CI end-to-end test before releasing.
package version

// Version is overridden at build time with -ldflags "-X .../version.Version=v1.0.0".
var Version = "0.1.0-dev"

// Images deployed by `observa central install` and `observa agent install`.
// Tags were checked against the upstream release tags on 2026-10-28.
var Images = struct {
	Grafana      string
	Prometheus   string
	Alertmanager string
	Loki         string
	Tempo        string
	Caddy        string
	Alloy        string
}{
	Grafana:      "grafana/grafana:13.3.0",
	Prometheus:   "prom/prometheus:v3.15.1",
	Alertmanager: "prom/alertmanager:v0.34.1",
	Loki:         "grafana/loki:3.7.9",
	Tempo:        "grafana/tempo:3.1.1",
	Caddy:        "caddy:2.11",
	Alloy:        "grafana/alloy:v1.21.2",
}
