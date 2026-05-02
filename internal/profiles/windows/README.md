# profiles/windows

The Windows profile (M6.A). Conduit's Windows deployment shape: the agent runs as a Windows Service in the host's process tree, scrapes host metrics + the Application + System Event Log channels by default, and forwards to Honeycomb (or a customer gateway).

## Fragments shipped

| File | Concern | Loaded when |
|---|---|---|
| [`hostmetrics.yaml`](hostmetrics.yaml) | Per-host CPU / memory / disk / filesystem / network / paging / processes scraping with the same `*.utilization` opt-ins as the linux fragment, so dashboards keyed off `system.*` work cross-platform. The `load` scraper on Windows reads the Performance Counter `System\Processor Queue Length` and emits it as `system.cpu.load_average.1m` (no 5m / 15m on Windows); the windows-host-overview board surfaces it as the saturation indicator. | `profile.mode: windows` (or `auto` on a Windows host) and `profile.host_metrics` is unset or `true`. |
| [`logs.yaml`](logs.yaml) | Two `windowseventlog` receiver instances reading the **Application** (third-party software) and **System** (OS / drivers / Windows services) channels. `start_at: end` so first-install doesn't replay weeks of historical events. | `profile.mode: windows` and `profile.system_logs` is unset or `true`. |

What this profile **does not** ship in V0:

- **`Security` Event Log channel**: the Security log requires `SeSecurityPrivilege` — granted to `LocalSystem` and to members of the "Event Log Readers" group when the right Group Policy is set. The MSI install (M6.C) registers the agent's service account; sites that want Security events configure the service account membership manually and add a Security receiver via the [`overrides:` escape hatch](../../../docs/adr/adr-0012.md). Shipping Security on by default would silently fail on most installs and quietly succeed on others — that's a bad telemetry surface.
- **filelog for IIS / app-specific logs**: Windows has no `/var/log` equivalent — application logs live under `%PROGRAMDATA%\<vendor>\`, IIS at `C:\inetpub\logs\LogFiles\`, etc. — so a one-size-fits-all default would be wrong. Operators add their paths via `overrides:`.

## What the profile does outside the fragment loader

1. **OTLP receivers stay on `127.0.0.1`**: the agent runs in the host's process tree (no container namespace), so peer apps reach the agent through loopback. Operators who need ingress from other hosts on the same Windows network override the bind address explicitly — same posture as the linux / darwin profiles.
2. **`health_check` extension on `0.0.0.0:13133`**: universal across every Conduit deployment via [`internal/expander/templates/base.yaml.tmpl`](../../expander/templates/base.yaml.tmpl). Useful for Windows Service Control Manager health probes.

## Profile contract status

[`PROFILE_SPEC.md`](../PROFILE_SPEC.md) §1 ("Telemetry the profile MUST emit") status:

| Section | M6.A status |
|---|---|
| Resource attributes | `host.name`, `host.id`, `os.type=windows`, `os.description`, `host.arch` provided by `resourcedetectionprocessor` (universal). |
| Host metrics | **Done.** `system.cpu.{usage,utilization}`, `system.memory.{usage,utilization}`, `system.filesystem.{usage,utilization}`, `system.disk.io`, `system.network.io`, `system.paging.{usage,utilization}`, `system.cpu.load_average.1m` (Processor Queue Length), `system.processes.count`. |
| Logs | **Done** for Application + System Event Log channels. Each event arrives with `winlog.channel`, `winlog.event_id`, `winlog.provider_name`, `winlog.computer`, `winlog.task` resource / record attributes — the windows-host-overview board breaks panels down on those columns. |

The dashboard quality bar (`PROFILE_SPEC.md` §3) ships at M6.D as [`dashboards/windows-host-overview.json`](../../../dashboards/windows-host-overview.json) — a Windows-native opinionated board (CPU queue length as the saturation indicator instead of Unix load average; log narrative organized by Event Log channel + provider rather than filelog templates).

## See also

- [`deploy/windows/README.md`](../../../deploy/windows/README.md) — Windows Service installation: MSI, install.ps1, and the `--service` entry point added in M6.B.
- [`internal/expander/expander.go`](../../expander/expander.go) §`resolvePlatform` for how `profile.mode: windows` resolves into the loader call against this directory.
