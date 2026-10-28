# Mac and Windows

Docker only runs natively on Linux. On Mac and Windows, Docker Desktop creates a hidden Linux virtual machine, and your containers run inside it. ObservaBoard detects this automatically: `observa doctor` reports it, and the agent adapts.

## What works under Docker Desktop

- `observa up --all-in-one --with-example`: central, agent, example application.
- Logs, container metrics, OpenTelemetry traces (level 2), HTTP metrics, alerts, dashboards.
- `observa init` / `apply` on your projects.

## What changes

- **"Host" metrics:** they are the Docker VM's, not your Mac's or PC's. The agent does not mount `/`, `/proc` or `/var/lib/docker`, which Docker Desktop does not share.
- **eBPF:** it depends on the VM's kernel, with no guarantee. Prefer level 2 (OpenTelemetry), which gives more information anyway.

## Testing eBPF reliably

Use a real Linux VM:

- **Mac:** OrbStack, Lima or Colima.
- **Windows:** install Docker Engine *inside* a WSL2 distribution (Ubuntu) rather than Docker Desktop, then run `observa` from that distribution.

Then: `observa up --all-in-one --with-example --ebpf`.

## Windows

- **Binary:** it is available (`observa-windows-amd64.exe`).
- **Docker commands:** use PowerShell or WSL2 to run the `docker compose` commands printed by the CLI.
- **All-in-one data:** it is stored in `%USERPROFILE%\.observa\local`.
