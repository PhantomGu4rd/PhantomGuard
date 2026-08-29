[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$binaryDirectory = Join-Path $repositoryRoot 'bin'
$binary = Join-Path $binaryDirectory 'phantomguard.exe'

New-Item -ItemType Directory -Force -Path $binaryDirectory | Out-Null
Push-Location $repositoryRoot
try {
    go build -trimpath -o $binary ./cmd/phantomguard
    if ($LASTEXITCODE -ne 0) {
        throw 'Go could not build the PhantomGuard development binary.'
    }
} finally {
    Pop-Location
}

$existing = @($env:Path -split ';' | Where-Object {
    $_ -and $_.TrimEnd('\') -ine $binaryDirectory.TrimEnd('\')
})
$env:Path = (@($binaryDirectory) + $existing) -join ';'

Write-Host "Prepared development binary: $binary"
Write-Host 'The current PowerShell session now resolves phantomguard to this Go binary first.'
Write-Host 'Check: Get-Command phantomguard -All'
