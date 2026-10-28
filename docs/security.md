# Security

## What is exposed

- **Central:** only Caddy publishes ports (80 and 443, or `127.0.0.1:3000` in all-in-one mode). Prometheus, Loki, Tempo, Alertmanager and Grafana stay on an internal Docker network.
- **Agents:** no published port. They push to the central. The agent's OTLP receiver is reachable only from the `observa` Docker network.

## Agent authentication

- Each agent has a **random 256-bit token**, in the `<agent>.<secret>` format, created by `observa token create`. It is displayed only once.
- **On the central**, only a bcrypt hash is kept (`tokens.yaml`, 0600). Caddy checks the identifier and secret on every push (HTTP basic auth over HTTPS).
- **On the agent**, the secret is in `.env` (0600), read through an environment variable. It is never written to `config.alloy`.
- **Revocation:** `observa token revoke <agent>`. It takes effect immediately: the command reloads Caddy, which mounts its configuration directory and therefore reads the new version.
- **Passing the token:** prefer `--token-file <file>`, `--token-file -` (standard input) or the `OBSERVA_AGENT_TOKEN` variable over `--token`, which stays in the shell history and is visible in `ps`.
- **bcrypt cost:** it is intentionally minimal. The secret is random and long, so a high cost would add no protection and would slow down every request.

In `--ip` mode, the certificate comes from Caddy's internal authority. Give it to the agents with `--ca-file`. Only use `--insecure-skip-verify` for a test.

## Agent privileges

The agent needs broad access to the host, and you should know it:

| Access | Reason |
| --- | --- |
| Docker socket (read-only) | discovering containers and reading their logs. **Access to the Docker socket is equivalent to root access on the host.** |
| `privileged` container | required by cAdvisor to read cgroups and disks |
| `/`, `/proc`, `/sys`, `/var/lib/docker` read-only | host and container metrics |
| `pid: host` (only with `--ebpf`) | eBPF observes the host's processes |

Only install the agent on servers you administer, and keep its image up to date (`observa agent install`, with a new version of the CLI).

## Isolation between projects and between agents (V1 limitations)

- **Shared network:** on a given server, all monitored services join the `observa` Docker network, with a predictable alias (`<project>-<env>-<service>`). A container from one project can therefore reach the database of another project on the same server. Do not host on the same server client projects that must not see each other, or protect each database with its own password (which is needed anyway). One network per project is planned for V2.
- **Labels declared by the agent:** the central authenticates each agent, but it does not check the labels the agent sends. A compromised agent could impersonate another server with a fake `host` label, or publish thresholds that hide another project's alerts. Treat every server running an agent as trusted. V2 multi-tenancy will enforce labels on the central based on the token.

## Sensitive data

- **Logs:** they are collected as is. Avoid logging secrets, card data or passwords. Automatic pattern masking is planned for V2.
- **Traces:** they contain routes, route parameters and SQL queries. Values of parameterized SQL queries are not recorded by default by the OpenTelemetry instrumentations.
- **Secrets:** database secrets (exporter access) stay on the agent's server (`.env`, 0600, values in single quotes so `$` and `#` are read literally). Notification channel secrets stay on the central, whose directory is 0700. No secret goes into `observa.yaml`.

## Grafana

- **Password:** the admin password is random and generated at install time. Change it in Grafana after the first login. The `.env` file is read only on first startup.
- **Access:** sign-up and anonymous access are disabled.
