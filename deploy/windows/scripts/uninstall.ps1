<#
.SYNOPSIS
  Conduit Windows uninstaller (M6.C).

.DESCRIPTION
  Stops the conduit service, runs `msiexec /x` against the registered
  product code, and verifies the service registration is gone. Operator
  edits to %PROGRAMDATA%\Conduit\conduit.{yaml,env} are preserved
  (NeverOverwrite components in the WiX source).

  To wipe the data dir too, pass -PurgeData. That deletes
  %PROGRAMDATA%\Conduit\ entirely — make sure you've copied
  conduit.yaml / conduit.env first if you'll reinstall.

.PARAMETER PurgeData
  Optional. Remove %PROGRAMDATA%\Conduit\ after the MSI uninstall.

.EXAMPLE
  PS> .\uninstall.ps1
  PS> .\uninstall.ps1 -PurgeData
#>
[CmdletBinding()]
param(
    [switch]$PurgeData
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$identity  = [System.Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object System.Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([System.Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "uninstall.ps1 must run as Administrator."
}

# Find the installed product. The MSI's UpgradeCode is fixed in
# deploy/windows/wix/conduit.wxs; we resolve the current ProductCode
# via WMI rather than hard-coding it (the ProductCode rotates per
# version build).
$product = Get-CimInstance -ClassName Win32_Product -Filter "Name='Conduit Agent'" -ErrorAction SilentlyContinue
if (-not $product) {
    Write-Host "Conduit Agent is not installed; nothing to uninstall."
    if ($PurgeData -and (Test-Path "$env:PROGRAMDATA\Conduit")) {
        Remove-Item -Recurse -Force "$env:PROGRAMDATA\Conduit"
        Write-Host "$env:PROGRAMDATA\Conduit removed."
    }
    exit 0
}

# Stop the service first so msiexec doesn't have to fight an in-flight
# binary lock.
$svc = Get-Service -Name conduit -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -ne 'Stopped') {
    Stop-Service -Name conduit -Force
}

$logPath = Join-Path $env:TEMP "conduit-uninstall.log"
Write-Host "Uninstalling $($product.Name) $($product.Version) (log: $logPath)"
$msiArgs = @(
    "/x", $product.IdentifyingNumber,
    "/qn",
    "/norestart",
    "/l*v", "`"$logPath`""
)
$proc = Start-Process -FilePath "msiexec.exe" -ArgumentList $msiArgs -Wait -PassThru -NoNewWindow
if ($proc.ExitCode -ne 0) {
    throw "msiexec /x failed with exit code $($proc.ExitCode); see $logPath"
}

# Sanity check: service registration is gone.
$svc = Get-Service -Name conduit -ErrorAction SilentlyContinue
if ($svc) {
    Write-Warning "service 'conduit' still exists after uninstall; the MSI may not have removed it cleanly. Inspect $logPath."
} else {
    Write-Host "service 'conduit' is no longer registered."
}

if ($PurgeData -and (Test-Path "$env:PROGRAMDATA\Conduit")) {
    Remove-Item -Recurse -Force "$env:PROGRAMDATA\Conduit"
    Write-Host "$env:PROGRAMDATA\Conduit removed."
} elseif (Test-Path "$env:PROGRAMDATA\Conduit") {
    Write-Host "$env:PROGRAMDATA\Conduit preserved (re-run with -PurgeData to wipe)."
}
