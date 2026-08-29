[CmdletBinding()]
param(
    [string]$SourcePath = (Join-Path $PSScriptRoot 'phantomguard.exe'),
    [string]$InstallDir = $(if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'PhantomGuard\bin' } else { Join-Path $HOME 'AppData\Local\PhantomGuard\bin' }),
    [switch]$NoPathUpdate
)

$ErrorActionPreference = 'Stop'

function Set-PhantomGuardPathFirst {
    param(
        [AllowEmptyString()]
        [string]$PathValue,
        [Parameter(Mandatory)]
        [string]$InstallDirectory
    )

    $normalizedInstallDirectory = $InstallDirectory.TrimEnd('\')
    $items = @($PathValue -split ';' | Where-Object { $_ })
    $withoutInstallDirectory = @($items | Where-Object {
        $_.TrimEnd('\') -ine $normalizedInstallDirectory
    })
    return (@($InstallDirectory) + $withoutInstallDirectory) -join ';'
}

if (-not (Test-Path -LiteralPath $SourcePath -PathType Leaf)) {
    throw "PhantomGuard binary not found: $SourcePath"
}

$resolvedSource = (Resolve-Path -LiteralPath $SourcePath).Path
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$resolvedInstallDir = (Resolve-Path -LiteralPath $InstallDir).Path
$destination = Join-Path $resolvedInstallDir 'phantomguard.exe'
Copy-Item -LiteralPath $resolvedSource -Destination $destination -Force

if (-not $NoPathUpdate) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $updatedPath = Set-PhantomGuardPathFirst -PathValue $userPath -InstallDirectory $resolvedInstallDir
    if ($updatedPath -ne $userPath) {
        [Environment]::SetEnvironmentVariable('Path', $updatedPath, 'User')
    }
    $env:Path = Set-PhantomGuardPathFirst -PathValue $env:Path -InstallDirectory $resolvedInstallDir
}

Write-Host "Installed PhantomGuard at $destination"
if ($NoPathUpdate) {
    Write-Host "Add $resolvedInstallDir to PATH before running phantomguard."
} else {
    Write-Host 'Run: phantomguard --help'
    Write-Host 'PhantomGuard was placed before similarly named commands in this PowerShell session and your user PATH.'
}
