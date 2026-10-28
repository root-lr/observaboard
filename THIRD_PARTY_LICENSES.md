# Third-party licenses

ObservaBoard itself is released under the [MIT license](LICENSE).

It relies on three kinds of third-party software, each under its own license:

1. **Code compiled into the `observa` binary**: a few vendored Go packages.
2. **Container images that `observa` deploys**: these are pulled unmodified from their official registries when you run `observa`. ObservaBoard does not redistribute them.
3. **Packages installed into your application images** by the generated `Dockerfile.observa` (OpenTelemetry SDKs), plus the example shop's dependencies.

This file is a summary, not legal advice. The license that applies is the one published by each project, linked below.

## 1. Compiled into the `observa` binary

| Component | Origin | Version | License |
| --- | --- | --- | --- |
| YAML parser | [go-yaml/yaml](https://github.com/go-yaml/yaml) (`gopkg.in/yaml.v3`) | v3.0.1 | MIT and Apache-2.0 ([text](internal/thirdparty/yaml/LICENSE), [NOTICE](internal/thirdparty/yaml/NOTICE)) |
| bcrypt | [golang/crypto](https://github.com/golang/crypto) (`golang.org/x/crypto/bcrypt`) | commit 1da6254a | BSD-3-Clause ([text](internal/thirdparty/bcrypt/LICENSE)) |
| blowfish | [golang/crypto](https://github.com/golang/crypto) (`golang.org/x/crypto/blowfish`) | commit 1da6254a | BSD-3-Clause ([text](internal/thirdparty/blowfish/LICENSE)) |
| Go standard library | [golang/go](https://github.com/golang/go) | Go 1.24+ | BSD-3-Clause |

Releases of the binary must keep these license texts available. They ship in the source tree under `internal/thirdparty/`.

## 2. Container images deployed by `observa`

Pinned versions come from [internal/version/version.go](internal/version/version.go).

| Component | Role | Image | License |
| --- | --- | --- | --- |
| [Grafana](https://github.com/grafana/grafana) | dashboards, runbooks | `grafana/grafana:13.3.0` | [AGPL-3.0](https://github.com/grafana/grafana/blob/main/LICENSE) |
| [Loki](https://github.com/grafana/loki) | logs | `grafana/loki:3.7.9` | [AGPL-3.0](https://github.com/grafana/loki/blob/main/LICENSE) |
| [Tempo](https://github.com/grafana/tempo) | traces, span metrics | `grafana/tempo:3.1.1` | [AGPL-3.0](https://github.com/grafana/tempo/blob/main/LICENSE) |
| [Prometheus](https://github.com/prometheus/prometheus) | metrics, alert rules | `prom/prometheus:v3.15.1` | [Apache-2.0](https://github.com/prometheus/prometheus/blob/main/LICENSE) |
| [Alertmanager](https://github.com/prometheus/alertmanager) | notification routing | `prom/alertmanager:v0.34.1` | [Apache-2.0](https://github.com/prometheus/alertmanager/blob/main/LICENSE) |
| [Caddy](https://github.com/caddyserver/caddy) | HTTPS, agent authentication | `caddy:2.11` | [Apache-2.0](https://github.com/caddyserver/caddy/blob/master/LICENSE) |
| [Grafana Alloy](https://github.com/grafana/alloy) | agent (includes the PostgreSQL and Redis exporters, cAdvisor, node exporter and Beyla for eBPF) | `grafana/alloy:v1.21.2` | [Apache-2.0](https://github.com/grafana/alloy/blob/main/LICENSE) |

**About AGPL-3.0 (Grafana, Loki, Tempo).** ObservaBoard only generates configuration for these programs and runs their official images unmodified. It does not include or link their code, so the MIT license of ObservaBoard is unaffected. If you **modify** one of these programs and let users interact with it over a network, the AGPL requires you to offer them the modified source code. Running the unmodified images, even for third parties, creates no such obligation.

## 3. Installed into application images

The generated `Dockerfile.observa` adds the OpenTelemetry SDK to your image:

| Component | Runtime | License |
| --- | --- | --- |
| [opentelemetry-distro, opentelemetry-exporter-otlp, opentelemetry-instrumentation-*](https://github.com/open-telemetry/opentelemetry-python-contrib) | Python | Apache-2.0 |
| [@opentelemetry/api, @opentelemetry/auto-instrumentations-node](https://github.com/open-telemetry/opentelemetry-js-contrib) | Node.js | Apache-2.0 |

### Example shop (`examples/django-shop`, demo only)

| Component | License |
| --- | --- |
| [PostgreSQL](https://www.postgresql.org/about/licence/) (`postgres:17-alpine`) | PostgreSQL License |
| [Redis](https://redis.io/legal/licenses/) (`redis:8-alpine`) | RSALv2, SSPLv1 or AGPL-3.0, at your choice (since Redis 8) |
| [Python](https://docs.python.org/3/license.html) (`python:3.12-slim`) | PSF-2.0 (base image: Debian packages under their own licenses) |
| [Django](https://github.com/django/django/blob/main/LICENSE) | BSD-3-Clause |
| [Celery](https://github.com/celery/celery/blob/main/LICENSE) | BSD-3-Clause |
| [Gunicorn](https://github.com/benoitc/gunicorn/blob/master/LICENSE) | MIT |
| [psycopg](https://github.com/psycopg/psycopg/blob/master/LICENSE.txt) | LGPL-3.0 |
| [dj-database-url](https://github.com/jazzband/dj-database-url/blob/master/LICENSE) | BSD-3-Clause |

## Trademarks

Grafana, Loki, Tempo and Alloy are trademarks of Grafana Labs. Prometheus is a trademark of the Linux Foundation. Redis is a trademark of Redis Ltd. PostgreSQL is a trademark of the PostgreSQL Community Association. ObservaBoard is an independent project and is not affiliated with or endorsed by any of them.
