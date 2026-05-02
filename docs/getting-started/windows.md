# Getting started — Windows

**Time to first signal**: ~15 minutes on a fresh Windows host. This
guide takes you from "I just provisioned a Windows Server 2019+ /
Windows 11 box" to "host metrics + Windows Event Log + traces are
landing in Honeycomb."

> **Status note**: M6 ships the source-side bits (WiX MSI, service
> entry point, install/uninstall scripts, dashboard). Authenticode
> signing on the MSI lands at M12 follow-up — until then, the MSI
> ships unsigned and Windows SmartScreen will warn on first install.
> Production rollout should wait for the signed MSI; this guide is
> the path for evaluation builds.

## What you'll have at the end

- A `Conduit` Windows Service running with `Start=Automatic` and
  auto-restart on failure.
- Host metrics (CPU, memory, filesystem, network, paging, processes)
  shipping to Honeycomb every 60 seconds.
- The "saturation" indicator on Windows is **CPU Queue Length**
  (the `system.cpu.load_average.1m` metric on the Windows profile;
  it reads the `System\Processor Queue Length` perf counter rather
  than Unix `loadavg`).
- Windows Event Log entries from the **Application** and **System**
  channels flowing as OTLP logs. The **Security** channel is
  intentionally not loaded by default — it requires
  `SeSecurityPrivilege` and most operators don't want auth events
  in their general telemetry.
- An OTLP receiver bound to `127.0.0.1:4317` / `:4318` for local apps.
- `conduit doctor` reports green.

## Prerequisites (5 min)

| Item | Where to get it |
|---|---|
| Windows Server 2019+ or Windows 10 1809+ / Windows 11 | — |
| **Administrator** PowerShell session | Right-click PowerShell → "Run as Administrator" |
| A Honeycomb ingest API key | [honeycomb.io](https://www.honeycomb.io) → API Keys |
| Internet egress to `api.honeycomb.io:443` | — |

## Step 1 — Install via PowerShell (5 min)

The unattended install script downloads the MSI, runs `msiexec /qn`,
sets the per-service registry `Environment` (so the service has
`HONEYCOMB_API_KEY` etc. on startup), restarts the service, and
probes the health endpoint:

```powershell
iwr -UseBasicParsing https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/deploy/windows/scripts/install.ps1 |
  iex

# Or to run the script directly (it requires Administrator):
.\install.ps1 `
  -ApiKey "$env:HONEYCOMB_API_KEY" `
  -ServiceName "edge-gateway" `
  -DeploymentEnvironment "production"
```

The script:

1. Downloads the latest release MSI from GitHub Releases (or you can
   pass `-MsiPath C:\path\to\conduit.msi` to use a local copy).
2. Runs `msiexec /i conduit.msi /qn /l*v install.log`.
3. Writes `HONEYCOMB_API_KEY`, `CONDUIT_SERVICE_NAME`, and
   `CONDUIT_DEPLOYMENT_ENVIRONMENT` into
   `HKLM:\SYSTEM\CurrentControlSet\Services\Conduit\Environment` so
   the service inherits them on startup (this is the per-service
   environment block; it never lands in the user's environment).
4. `Restart-Service Conduit`.
5. Polls `http://127.0.0.1:13133/` until the agent reports healthy.

For the script source and what it sets where, see
[`deploy/windows/scripts/install.ps1`](../../deploy/windows/scripts/install.ps1).

### Manual install

Download the MSI from [Releases](https://github.com/conduit-obs/conduit-agent/releases),
then:

```powershell
msiexec /i conduit-X.Y.Z.msi /qn /l*v install.log

# Set per-service environment:
$env_path = "HKLM:\SYSTEM\CurrentControlSet\Services\Conduit"
$multi = @(
  "HONEYCOMB_API_KEY=$env:HONEYCOMB_API_KEY",
  "CONDUIT_SERVICE_NAME=edge-gateway",
  "CONDUIT_DEPLOYMENT_ENVIRONMENT=production"
)
New-ItemProperty -Path $env_path -Name Environment -Value $multi `
  -PropertyType MultiString -Force | Out-Null

Restart-Service Conduit
```

The MSI installs:

| Path | What it is |
|---|---|
| `%PROGRAMFILES%\Conduit\conduit.exe` | The agent binary. |
| `%PROGRAMDATA%\Conduit\conduit.yaml` | Default config; references env vars. `NeverOverwrite="yes"` so upgrades preserve operator edits. |
| `%PROGRAMDATA%\Conduit\conduit.env.default` | Env-var template; the install scripts write the actual values to the per-service registry environment. |
| `Conduit` Windows Service | `Start=Automatic`, auto-restart on first / second / third failure. |

For internals (the WiX source, service registration, install script
internals), see [`deploy/windows/README.md`](../../deploy/windows/README.md).

## Step 2 — Verify (3 min)

Confirm the service is running:

```powershell
Get-Service Conduit | Format-List Name, Status, StartType
```

You should see `Status: Running` and `StartType: Automatic`. If
`Status: Stopped`, jump to [Troubleshooting](#troubleshooting) below.

Run the doctor:

```powershell
& "$env:ProgramFiles\Conduit\conduit.exe" doctor `
  -c "$env:ProgramData\Conduit\conduit.yaml"
```

Expect a clean run. The Windows profile checks:

- `CDT0001 config.syntax` — `conduit.yaml` parses cleanly.
- `CDT0102 output.auth` — the API key is set (the registry env
  block is read at service start, so the doctor sees it via
  `${env:HONEYCOMB_API_KEY}`).
- `CDT0201 receiver.ports` — `127.0.0.1:4317` / `:4318` are free.
- `CDT0202 receiver.permissions` — Windows Event Log channels are
  reachable (the `windowseventlogreceiver` doesn't have a filelog-
  style include list to check; the doctor reports PASS).

Tail the agent's stderr:

```powershell
Get-EventLog -LogName Application -Source "Conduit" -Newest 50 |
  Format-List TimeGenerated, EntryType, Message
```

The Windows service writes its own startup / shutdown events to the
Windows Event Log (under "Conduit" source).

## Step 3 — Confirm data in Honeycomb (3 min)

In Honeycomb, open the dataset matching `CONDUIT_SERVICE_NAME`
(here: `edge-gateway`):

| Where to look | What you'll see |
|---|---|
| **Datasets list** | A new entry: `edge-gateway` |
| **Query** → metric: `system.cpu.utilization`, group by `host.name` | Per-host CPU |
| **Query** → metric: `system.cpu.load_average.1m`, group by `host.name` | **CPU Queue Length** (the Windows native saturation indicator) |
| **Query** → log filter: `winlog.channel = "Application"` | Application Event Log |
| **Query** → log filter: `winlog.channel = "System"` | System Event Log |

The shipped [`dashboards/windows-host-overview.json`](../../dashboards/windows-host-overview.json)
gives you a two-narrative board (per-host resource pressure +
Windows Event Log analysis keyed off `winlog.channel`,
`winlog.provider_name`, `winlog.event_id`) — import via Honeycomb's
`Boards → Import`.

## Step 4 — Send traces from local apps

The OTLP receiver is bound to `127.0.0.1:4317` / `:4318`. Point any
local app's OTel SDK at `http://127.0.0.1:4318` (HTTP) or
`127.0.0.1:4317` (gRPC).

For .NET apps, the canonical recipe is:

```xml
<!-- appsettings.json or env vars -->
"Otlp": {
  "Endpoint": "http://127.0.0.1:4318/v1/traces",
  "Protocol": "HttpProtobuf"
}
```

For PowerShell-instrumented scripts via the OpenTelemetry .NET SDK:

```powershell
$env:OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4318"
$env:OTEL_RESOURCE_ATTRIBUTES = "service.name=checkout,deployment.environment=production"
```

The agent injects the Honeycomb API key on egress.

## Step 5 — Enable the Security Event Log (advanced)

The default Windows profile excludes the Security channel because
reading it requires `SeSecurityPrivilege` (and most operators
shouldn't be sending auth events into general telemetry).

If you do want it, drop a small overrides file:

```yaml
# %PROGRAMDATA%\Conduit\conduit.yaml — overrides: section
overrides:
  receivers:
    windowseventlog/security:
      channel: security
      start_at: end
  service:
    pipelines:
      logs:
        receivers: [otlp, windowseventlog/application, windowseventlog/system, windowseventlog/security]
```

Then add the agent's service account (`LocalSystem` by default) to
the local **Manage auditing and security log** policy
(`secpol.msc → Local Policies → User Rights Assignment`) and
restart the service.

The overrides escape hatch (ADR-0012) is the right mechanism — we
don't promote Security-channel ingestion to a first-class flag
because most operators shouldn't have it on.

## Troubleshooting

### Service shows `Status: Stopped` after install

```powershell
Get-EventLog -LogName Application -Source "Conduit" -Newest 20 |
  Format-List TimeGenerated, EntryType, Message
```

The most common causes:

- **Empty / wrong API key**: the per-service registry `Environment`
  wasn't written. Re-run the install script, or write it manually
  per [Step 1 → Manual install](#manual-install).
- **Port conflict on 4317 / 4318**: another collector is already
  running. The doctor's CDT0201 check would surface this; on
  Windows you can also `Get-NetTCPConnection -LocalPort 4317`.
- **`conduit.yaml` invalid**: rare on a fresh install but possible
  after manual edits. Run
  `& "$env:ProgramFiles\Conduit\conduit.exe" config --validate -c "$env:ProgramData\Conduit\conduit.yaml"`.

### CDT0101 output.endpoint_reachable failure

Most Windows hosts ship with the Mozilla CA bundle and reach
Honeycomb fine. If you see TLS verification failures, your host is
behind a corporate MITM proxy. Add the internal CA's certificate to
the **Trusted Root Certification Authorities** store
(`certlm.msc → Trusted Root Certification Authorities`) and
restart the service.

### Windows Defender / SmartScreen blocks the unsigned MSI

This is expected until M12 ships Authenticode signing. Click "More
info" → "Run anyway" on the SmartScreen dialog, or use the
PowerShell installer (which downloads the MSI under the hood; same
dialog if you're on Windows 11 with smart filtering enabled).

After Authenticode signing lands, this section disappears and
SmartScreen will recognize the signature.

## Uninstall

```powershell
& "C:\path\to\uninstall.ps1"
```

Or manually:

```powershell
$code = (Get-WmiObject Win32_Product -Filter "Name='Conduit'").IdentifyingNumber
msiexec /x $code /qn /l*v uninstall.log
```

Add `-PurgeData` (PowerShell script) or manually
`Remove-Item -Recurse $env:ProgramData\Conduit\` to also remove the
config + persistent queue directory.

## Next steps

- [**Configuration reference**](../reference/configuration.md) — the
  full `conduit.yaml` schema.
- [**Architecture overview**](../architecture/overview.md) — what's
  inside the agent on Windows specifically.
- [**Troubleshooting index**](../troubleshooting/index.md) — every
  CDT code with fix steps.
