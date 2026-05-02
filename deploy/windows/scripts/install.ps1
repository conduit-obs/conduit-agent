<#
.SYNOPSIS
  Conduit Windows unattended installer (M6.C).

.DESCRIPTION
  Wrapper around the Conduit MSI that handles the two things the MSI
  alone can't sensibly do:

    1. Resolve the latest release version from GitHub and download the
       per-arch MSI to a temp directory (or accept a -MsiPath to a
       locally-cached file for air-gapped installs).
    2. Set the conduit Windows Service's environment variables (the
       MSI registers the service, but the SCM gives services an empty
       environment by default — HONEYCOMB_API_KEY and friends have to
       be threaded through the per-service registry value
       HKLM\SYSTEM\CurrentControlSet\Services\conduit\Environment).

  After install, the service is started; its health endpoint is reachable
  at http://127.0.0.1:13133/ and a `Get-Service conduit` shows Running.

.PARAMETER HoneycombApiKey
  Required. The Honeycomb ingest key (hcaik_…).

.PARAMETER ServiceName
  Optional. service.name resource attribute on every emitted signal.
  Default: $env:COMPUTERNAME

.PARAMETER DeploymentEnvironment
  Optional. deployment.environment resource attribute. Default:
  "production".

.PARAMETER MsiPath
  Optional. Path to a locally-cached conduit-<version>-<arch>.msi.
  When omitted, the script downloads the latest release MSI from
  github.com/conduit-obs/conduit-agent/releases.

.PARAMETER Version
  Optional. Pin a specific release version (e.g. "0.1.0"). When
  omitted, the script resolves the GitHub "latest" release.

.EXAMPLE
  # Most common: latest release, prompted for the API key
  PS> .\install.ps1 -HoneycombApiKey hcaik_xxx

.EXAMPLE
  # Air-gapped: pre-staged MSI, scripted env
  PS> .\install.ps1 -MsiPath C:\stage\conduit-0.1.0-x86_64.msi `
                    -HoneycombApiKey hcaik_xxx `
                    -ServiceName edge-host -DeploymentEnvironment production
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$HoneycombApiKey,

    [string]$ServiceName = $env:COMPUTERNAME,
    [string]$DeploymentEnvironment = "production",
    [string]$MsiPath,
    [string]$Version
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

# Require admin: MSI install + service registration both need it; failing
# fast here is friendlier than a half-applied install.
$identity  = [System.Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object System.Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "install.ps1 must run as Administrator. Re-launch from an elevated PowerShell."
}

# Resolve MSI: either operator-supplied path or download from GitHub.
if (-not $MsiPath) {
    $arch = if ([Environment]::Is64BitOperatingSystem) { "x86_64" } else { "i386" }
    $repo = "conduit-obs/conduit-agent"
    if (-not $Version) {
        $latest  = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
        $Version = $latest.tag_name -replace '^v', ''
    }
    $msiName = "conduit-$Version-$arch.msi"
    $url     = "https://github.com/$repo/releases/download/$Version/$msiName"
    $MsiPath = Join-Path $env:TEMP $msiName
    Write-Host "Downloading $url -> $MsiPath"
    Invoke-WebRequest -Uri $url -OutFile $MsiPath -UseBasicParsing
}
if (-not (Test-Path $MsiPath)) {
    throw "MSI not found at $MsiPath."
}

# msiexec /qn = silent, /norestart = no auto-reboot, /l*v = verbose log.
# The log lives next to the MSI for postmortem if the install fails.
$logPath = "$MsiPath.install.log"
Write-Host "Installing $MsiPath (log: $logPath)"
$msiArgs = @(
    "/i", "`"$MsiPath`"",
    "/qn",
    "/norestart",
    "/l*v", "`"$logPath`""
)
$proc = Start-Process -FilePath "msiexec.exe" -ArgumentList $msiArgs -Wait -PassThru -NoNewWindow
if ($proc.ExitCode -ne 0) {
    throw "msiexec failed with exit code $($proc.ExitCode); see $logPath"
}

# Set service environment via the per-service registry value. The SCM
# reads this REG_MULTI_SZ at service start; values are KEY=VALUE strings.
# A service restart applies the new values.
$svcKey = "HKLM:\SYSTEM\CurrentControlSet\Services\conduit"
if (-not (Test-Path $svcKey)) {
    throw "service registry key $svcKey not found; the MSI install may have failed."
}
$envEntries = @(
    "HONEYCOMB_API_KEY=$HoneycombApiKey",
    "CONDUIT_SERVICE_NAME=$ServiceName",
    "CONDUIT_DEPLOYMENT_ENVIRONMENT=$DeploymentEnvironment"
)
Set-ItemProperty -Path $svcKey -Name "Environment" -Type MultiString -Value $envEntries
Write-Host "Service environment set on $svcKey\Environment"

# The MSI started the service with an empty env; restart so the values
# above land in the running process.
Restart-Service -Name conduit
$svc = Get-Service -Name conduit
Write-Host "conduit service status: $($svc.Status)"
if ($svc.Status -ne "Running") {
    Write-Warning "conduit service is not Running; check the Application Event Log under provider=conduit and the install log at $logPath"
    exit 1
}

# Liveness probe. The collector binds health_check on 0.0.0.0:13133;
# from localhost, http://127.0.0.1:13133/ returns 200 once every
# pipeline started cleanly.
try {
    $resp = Invoke-WebRequest -Uri "http://127.0.0.1:13133/" -UseBasicParsing -TimeoutSec 10
    if ($resp.StatusCode -eq 200) {
        Write-Host "Conduit health endpoint OK (200 from http://127.0.0.1:13133/)"
    } else {
        Write-Warning "Health endpoint returned status $($resp.StatusCode)"
    }
} catch {
    Write-Warning "Health endpoint not reachable: $($_.Exception.Message)"
}

Write-Host ""
Write-Host "Conduit installed. Useful follow-ups:"
Write-Host "  Get-Service conduit                                      # current status"
Write-Host "  Get-EventLog -LogName Application -Source conduit -Newest 20"
Write-Host "  conduit doctor                                           # M11; see CDT0xxx checks"
