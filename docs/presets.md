# Presets and languages

A preset bundles what ObservaBoard knows about a technology:
- how to recognize it;
- how to instrument it;
- which dashboard displays it;
- which alerts apply to it, and with which default thresholds.

`observa preset list` and `observa preset show <name>` display this information.

## V1 presets

| Preset | Dashboard | Instrumentation | Main data |
| --- | --- | --- | --- |
| `django` | HTTP service | zero-code OpenTelemetry Python | requests per route, errors, p50/p95/p99 latencies, SQL time, outgoing calls, slow and failed traces |
| `fastapi` | HTTP service | zero-code OpenTelemetry Python | same |
| `celery` | Celery worker | OpenTelemetry Python (tasks = *consumer* spans) | tasks/s by type, failures, p95 duration, Redis queue, active workers |
| `postgres` | PostgreSQL | agent exporter | connections by state, transactions, cache, rows, locks, size |
| `redis` | Redis | agent exporter | memory, clients, commands, hit rate, evictions, Celery queue |
| `generic` | Service · container | none, or zero-code Node.js, or eBPF | availability, CPU, memory, network, restarts, logs, HTTP if instrumented |

Every dashboard shows the service logs. A line containing `trace_id=…` opens the matching trace.

## Support matrix

| Technology | Level 1 (logs, resources) | Traces and HTTP metrics | Dedicated preset |
| --- | --- | --- | --- |
| Python: Django, FastAPI, Flask, Odoo… | V1 | V1 (zero-code) | Django, FastAPI, Celery in V1 |
| Node.js: NestJS, Express, Next.js server | V1 | V1 (zero-code, Node 22.15+ for the latest instrumentation release) | V2 |
| Java, PHP, .NET, Ruby | V1 | eBPF in V1, zero-code in V2 | V2 |
| Go, Rust, others | V1 | eBPF in V1, manual SDK | after V2 |
| PostgreSQL, Redis/Valkey | V1 | — | V1 |
| MySQL, MongoDB, RabbitMQ, Nginx | V1 | — | V2 |

## How zero-code instrumentation works

**Python.** The derived image installs `opentelemetry-distro`, `opentelemetry-exporter-otlp` and, through `opentelemetry-bootstrap -a install`, the instrumentations for the libraries present: Django, psycopg, requests, redis, celery… The command is prefixed with `opentelemetry-instrument`. Python subprocesses started by an `sh -c` inherit the instrumentation.

**Node.js.** The derived image installs `@opentelemetry/auto-instrumentations-node`. `NODE_OPTIONS` gets `--require @opentelemetry/auto-instrumentations-node/register`; an existing value is kept.

**Variables set by the override.** The most important ones:

| Variable | Value |
| --- | --- |
| `OTEL_SERVICE_NAME` | service name |
| `OTEL_RESOURCE_ATTRIBUTES` | `observa.project`, `observa.env`, `observa.service`, `observa.preset` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://observa-agent:4318` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (the Python distro uses gRPC by default) |
| `OTEL_SEMCONV_STABILITY_OPT_IN` | `http` (stable HTTP conventions: `http.route`, `http.response.status_code`) |
| `OTEL_METRICS_EXPORTER` | `none` (see [architecture](architecture.md#why-http-metrics-come-from-traces)) |
| `OTEL_PYTHON_LOG_CORRELATION` | `true` |
| `OTEL_PYTHON_EXCLUDED_URLS` | the service's `healthcheck` |

## Correlating logs and traces in Python

`OTEL_PYTHON_LOG_CORRELATION=true` adds `otelTraceID` and `otelSpanID` to every log record. The default format does not apply if your application configures its own logging, which Django does with `LOGGING`. In that case, add these fields to your formatter:

```python
"format": "%(asctime)s %(levelname)s %(name)s trace_id=%(otelTraceID)s %(message)s"
```

The example `examples/django-shop/shop/settings.py` shows a complete configuration, with a filter that avoids the error when OpenTelemetry is not active.

## Gunicorn and multi-process servers

Traces are exported correctly by each worker. The SDK restarts its export threads after the `fork`, which was verified with 3 workers. Avoid the `--preload` option, which initializes the application before the fork.

## Writing a preset

A preset touches four places:

1. `internal/presets/presets.go`: name, dashboard, alerts, thresholds (`Param`), optional exporter.
2. `internal/detect/detect.go`: recognition rules (image, dependency, command).
3. `internal/dashboards/dashboards.go`: its dashboard.
4. `assets/central/prometheus/rules.yml.tmpl` and `assets/runbooks/`: its alerts and their runbooks.

Tests check that everything is consistent:
- every alert of a preset exists;
- every alert has its runbook;
- every threshold has a default value;
- every `observa:*` series used is actually recorded.

Run `make test test-rules`.
