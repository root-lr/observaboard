# Development

## Repository layout

```text
cmd/observa/              CLI entry point
internal/
  cli/                    commands (one file per family: central, agent, project, up, doctor)
  central/                central settings, tokens, rendering of all configurations, Alertmanager
  agent/                  agent settings, applied projects, attach, config.alloy rendering, thresholds
  project/                observa.yaml model and validation
  compose/                reading compose files and Dockerfiles
  detect/                 preset/runtime/port detection per service
  initgen/                generation of the compose override and derived Dockerfiles
  instrument/             OpenTelemetry variables, instrumented command, Dockerfile lines
  presets/                per-technology knowledge (thresholds, alerts, dashboards)
  dashboards/             Grafana dashboards written in Go (builder + definitions)
  runbook/                Markdown → HTML conversion of the runbooks
  dockerx/                docker / docker compose calls (--dry-run mode)
  fsutil/                 atomic file writes
  version/                CLI version and pinned images
  thirdparty/             vendored yaml.v3 and bcrypt (see their README)
assets/
  central/                templates: compose, Caddyfile, Prometheus (+ rules), Loki, Tempo, Grafana
  agent/                  templates: config.alloy, agent compose
  runbooks/               one Markdown runbook per alert
examples/django-shop/     example shop (Django, PostgreSQL, Redis, Celery, traffic, simulated failures)
tests/rules/              promtool unit tests for the alert rules
tests/e2e/                end-to-end test (Docker required)
```

The Go module is `github.com/root-lr/observaboard`. If you publish under another account, replace this path everywhere: `go.mod`, imports, README, `install.sh`.

## Tests

| Command | What it checks | Requirements |
| --- | --- | --- |
| `make test` | `go vet` and Go tests: validation, detection on the example, generated override, agent and central rendering, valid YAML/JSON, alerts ↔ runbooks ↔ presets ↔ thresholds consistency, dashboards, Alertmanager routing, tokens, full CLI flows in `--dry-run` | Go |
| `make test-rules` | alert behavior with `promtool test rules`: default and overridden thresholds, routing, service down, silent agent, PostgreSQL, Celery queue, failed tasks | promtool or Docker |
| `make test-configs` | every generated configuration validated by the component's official binary, at the pinned versions: promtool, amtool, `caddy validate`, `loki -verify-config`, `alloy fmt`, `docker compose config` | Docker |
| `make e2e` | real stack, example application, metrics/logs/traces arriving, simulated failure, alert firing | Docker, 15 min |

GitHub Actions CI (`.github/workflows/ci.yml`) runs all four. A `v*` tag publishes the binaries.

### Code review before release

An independent review found 2 blocking and 11 major bugs, all fixed and covered by tests:
- **Blocking:** single-file mounts (hot reloads not working).
- **Blocking:** read permissions on `alertmanager.yml`.
- **Generation:** `network_mode`, multi-stage Dockerfiles with `target` and inherited `USER`, `ENTRYPOINT` to instrument, Dockerfile `$` to escape, service names with `_`, implicit Compose override.
- **Agent and CLI:** `.env` values to wrap in single quotes, default directories after an all-in-one, options on reinstall, conflicting routing between two agents, duplicate eBPF port, IPv6, `localhost`, email, reading standard input.

### What was verified while writing V1

The development environment had no Docker daemon, so the following checks were done with the binaries available:

- **Tests:** Go tests and 8 `promtool test rules` scenarios pass. The promtool tests were checked by mutation (a wrong expectation makes them fail).
- **Config validation:**
  - `promtool check rules`: 31 rules;
  - `amtool check-config` and `amtool config routes test`: per-channel routing, default, heartbeat;
  - `caddy validate`: Caddy 2.11.7, exact version;
  - Alloy 1.21.2's official parser on the generated `config.alloy` files;
  - `docker compose config` on the merge of the original compose + override;
  - 138 PromQL expressions parsed by a PromQL parser.
- **Real pipeline:** Prometheus agent and node_exporter → Caddy (token) → central Prometheus with the rules → Alertmanager. Verified: rejection without a token or with a wrong token, acceptance of the right one, hot revocation, runbooks served, all rules evaluated without errors, routing computed.
- **Real instrumentation:** the shop with 3 gunicorn workers, started with exactly the generated variables and command. Verified: all spans received, `observa.*` attributes, `http.route`, `http.response.status_code`, SQL spans, healthcheck excluded, `trace_id` in the logs.

**Not run here, covered by `make test-configs` and `make e2e` in CI:** actually starting Loki, Tempo, Grafana and Alloy in Docker, and validating Alloy component arguments beyond syntax.

## Upgrading a component

1. Change the version in `internal/version/version.go`, and in the `Makefile` for the validation images.
2. Read the component's changelog. Tempo 3 and Alloy change syntax regularly: the component's repository, at the target tag, is the reference.
3. Run `make test test-rules test-configs e2e`.

## Conventions

- **Language:** CLI messages, documentation, code and comments are in English.
- **Generated files:** they start with a "Generated by… do not edit" comment.
- **Writes:** every write goes through `fsutil.WriteFile` (atomic, and reports whether the content changed), so only what changed gets reloaded.
- **Secrets:** they go into files with mode 0600 and are never displayed twice.
