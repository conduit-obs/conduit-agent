# deploy/windows

Windows deployment artifacts for Conduit (M6). MSI installer, Windows Service registration, default config, install / uninstall scripts. Authenticode signing is a release-pipeline concern (M12 — gated on OQ-5 in the decision log).

## Files

| Path | Purpose |
|---|---|
| [`wix/conduit.wxs`](wix/conduit.wxs) | WiX 3.x source. Builds an MSI that installs `conduit.exe` to `%PROGRAMFILES%\Conduit\`, drops `conduit.yaml` + `conduit.env` (and `.default` reference copies) under `%PROGRAMDATA%\Conduit\`, and registers a Windows Service named `conduit` set to `auto` start with `DelayedAutoStart="yes"`. The live `conduit.yaml` / `conduit.env` carry `NeverOverwrite="yes"` so operator edits survive MSI upgrades; the `.default` companions get refreshed each upgrade for diffing. |
| [`conduit.yaml.default`](conduit.yaml.default) | Default `%PROGRAMDATA%\Conduit\conduit.yaml`. Sets `profile.mode: windows` so the expander loads the Application + System Event Log fragments and the Windows hostmetrics fragment. Ingest credentials come from `${env:HONEYCOMB_API_KEY}`, set via the per-service registry Environment by `install.ps1`. |
| [`conduit.env.default`](conduit.env.default) | Reference env file (commented placeholders). The actual service environment is set in the registry — `conduit.env` is preserved on disk for operators who want a checked-in record of what's set in their fleet. |
| [`scripts/install.ps1`](scripts/install.ps1) | Unattended installer. Downloads the latest release MSI from GitHub (or accepts a locally-staged path), invokes `msiexec /qn`, sets the per-service registry `Environment`, restarts the service, and probes `http://127.0.0.1:13133/` for liveness. |
| [`scripts/uninstall.ps1`](scripts/uninstall.ps1) | Stops the service, resolves the installed `ProductCode` via WMI, runs `msiexec /x`, and verifies the service registration is gone. `-PurgeData` also wipes `%PROGRAMDATA%\Conduit\`. |

## Quick start

From an elevated PowerShell on a Windows host with internet access:

```powershell
# Latest release; install.ps1 prompts for ($HoneycombApiKey is required)
.\install.ps1 -HoneycombApiKey hcaik_xxx
```

For air-gapped installs, pre-stage the MSI:

```powershell
.\install.ps1 -MsiPath C:\stage\conduit-0.1.0-x86_64.msi `
              -HoneycombApiKey hcaik_xxx `
              -ServiceName edge-host `
              -DeploymentEnvironment production
```

After install:

```powershell
Get-Service conduit                                         # Status: Running
Invoke-WebRequest http://127.0.0.1:13133/ -UseBasicParsing  # 200 OK
Get-EventLog -LogName Application -Source conduit -Newest 20
```

## What the MSI gives you

| Acceptance criterion (M6) | Where it's satisfied |
|---|---|
| MSI installs Conduit as a Windows Service set to auto-start | `<ServiceInstall Start="auto">` in [`wix/conduit.wxs`](wix/conduit.wxs); `<util:ServiceConfig>` adds first/second-failure restart with a 60s delay so transient failures self-heal. |
| Default profile ships Event Log to Honeycomb | [`conduit.yaml.default`](conduit.yaml.default) sets `profile.mode: windows`; the expander loads [`internal/profiles/windows/logs.yaml`](../../internal/profiles/windows/logs.yaml) (Application + System channels). |
| Uninstall removes service registration | `<ServiceControl Remove="uninstall">` in the WiX source; [`scripts/uninstall.ps1`](scripts/uninstall.ps1) verifies `Get-Service conduit` returns nothing post-uninstall. |
| MSI is signed | Deferred to M12. The Authenticode signing step runs after `light.exe` produces the .msi (signtool.exe / osslsigncode); EV cert procurement is gated on OQ-5. |
| PowerShell install script supports unattended install | [`scripts/install.ps1`](scripts/install.ps1) — non-interactive when all required parameters are passed. |
| `dashboards/windows-host-overview.json` validates | [`dashboards/windows-host-overview.json`](../../dashboards/windows-host-overview.json) (M6.D). |

## Service environment

Windows services start with an empty environment by default. Conduit reads `${env:HONEYCOMB_API_KEY}`, `${env:CONDUIT_SERVICE_NAME}`, and `${env:CONDUIT_DEPLOYMENT_ENVIRONMENT}` via the OTel Collector's env-substitution provider, so those values must reach the service somehow. Two paths:

1. **Per-service registry environment** (what `install.ps1` does):

   ```text
   HKLM\SYSTEM\CurrentControlSet\Services\conduit
     Environment (REG_MULTI_SZ):
       HONEYCOMB_API_KEY=hcaik_xxx
       CONDUIT_SERVICE_NAME=edge-host
       CONDUIT_DEPLOYMENT_ENVIRONMENT=production
   ```

   Restart the service after editing: `Restart-Service conduit`.

2. **System-wide environment**: `[Environment]::SetEnvironmentVariable("HONEYCOMB_API_KEY", "...", "Machine")` then `Restart-Service conduit`. Less granular but visible in Server Manager. Only use this when nothing else on the machine reads those env names — service-scoped registry is safer.

The `conduit.env` file under `%PROGRAMDATA%\Conduit\` is **not** auto-loaded — it's a checked-in reference of what's expected in the service environment. M11's `conduit doctor` warns if the live service environment differs from `conduit.env`.

## Build (release pipeline)

The MSI is built by goreleaser as part of `make release`. The build needs:

- WiX 3.x toolchain (`candle.exe` + `light.exe`) on Windows runners, or `msitools` (`wixl`) on linux/macOS runners.
- The compiled `conduit.exe` from the matching `goos: windows` builds entry.
- This `.wxs` source plus `conduit.yaml.default` / `conduit.env.default` from this directory.

Local snapshot build on Windows:

```powershell
goreleaser release --snapshot --clean --skip=publish
```

…produces `dist/conduit_<version>_windows_amd64.msi`. The first published version validates the WiX source end-to-end; all changes after that are reviewed against a kept golden MSI.

The signing step (Authenticode for both `conduit.exe` and the wrapping `.msi`) lands at M12 alongside the cosign / GPG signing decisions.

## See also

- [`internal/profiles/windows/`](../../internal/profiles/windows/README.md) — what the Windows profile loads at runtime.
- [`cmd/run/run_windows.go`](../../cmd/run/run_windows.go) — the SCM dispatch path that the MSI's `<ServiceInstall>` invokes.
- [`dashboards/windows-host-overview.json`](../../dashboards/windows-host-overview.json) — the default board (M6.D).
