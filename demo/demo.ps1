<#+
.SYNOPSIS
Runs the PhantomGuard hackathon demo from Windows PowerShell.

.DESCRIPTION
Use this native Windows version when WSL is unavailable. It has the same
behavior as demo.sh: verified phantom import is blocked, then a corrected
commit succeeds and the warm cache is shown.
#>

$ErrorActionPreference = "Stop"

foreach ($command in "git", "curl.exe", "phantomguard") {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "Demo prerequisite missing: $command"
    }
}

$root = Join-Path ([System.IO.Path]::GetTempPath()) ("phantomguard-demo-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $root | Out-Null

try {
    Push-Location $root
    & git init -q
    & git config user.email "demo@example.test"
    & git config user.name "PhantomGuardDemo"
    & phantomguard install

    $phantom = "phantomguard_nonexistent_dependency_" + [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() + "_xk9v"
    $registryName = $phantom.Replace("_", "-")
    $httpStatus = & curl.exe -sS --max-time 8 -o NUL -w "%{http_code}" "https://pypi.org/pypi/$registryName/json"
    if ($LASTEXITCODE -ne 0) {
        throw "Could not verify the demo package against PyPI; check network access."
    }
    if ($httpStatus -eq "200") {
        throw "Generated demo name unexpectedly exists; rerun."
    }
    if ($httpStatus -ne "404") {
        throw "PyPI returned HTTP $httpStatus instead of the required 404; rerun later."
    }

    [System.IO.File]::WriteAllText(
        (Join-Path $root "app.py"),
        "import flask`nimport requests`nimport $phantom`n"
    )
    & git add app.py
    Write-Host "Expected result: blocked, naming the phantom package at app.py:3."
    $previousErrorAction = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $blockedOutput = & git commit -m "AI generated dependency" 2>&1 | Out-String
    $blockedExitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousErrorAction
    Write-Host $blockedOutput
    if ($blockedExitCode -eq 0) {
        throw "ERROR: expected PhantomGuard to block commit"
    }

    "y" | & phantomguard fix --file app.py --from $phantom --to requests --ecosystem pypi
    & git add app.py
    & git commit -m "Use a verified dependency"
    $warm = Measure-Command { & phantomguard scan --staged }
    Write-Host ("Warm-cache scan: {0:N0} ms" -f $warm.TotalMilliseconds)
}
finally {
    Pop-Location -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
