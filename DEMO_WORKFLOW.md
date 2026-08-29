# PhantomGuard hackathon demo workflow

This Windows-first workflow exercises the development binary without relying on any globally installed `phantomguard` command. It is safe to show on video because the phantom-dependency demonstration happens in a new temporary Git repository, not in this source tree.

## 1. Prepare the Go development binary

From the repository root, run:

```powershell
$sourceRepo = (Get-Location).Path
.\scripts\prepare-dev.ps1
Get-Command phantomguard -All
phantomguard --version
```

The first result must be `...\Anti-Hallucination-Git-Hook-OpenAI-Build-Week\bin\phantomguard.exe`. If a Python command still appears later in the list, that is harmless: the Go binary is now first for this session. For a command that can never conflict, use `& .\bin\phantomguard.exe` in place of `phantomguard`.

For a persistent Windows installation, build once and run the installer. It places `%LOCALAPPDATA%\PhantomGuard\bin` before similarly named commands in both the current session and user PATH.

```powershell
.\installer\install.ps1 -SourcePath .\bin\phantomguard.exe
Get-Command phantomguard -All
```

## 2. Five-minute project tour

These commands are safe to run in this repository. The final command is the same strict index check used by the Git hook.

```powershell
phantomguard --help
phantomguard version
phantomguard cache status
phantomguard scan --all
phantomguard scan --staged --json
phantomguard tui --no-color --no-interactive
phantomguard verify --strict
```

Open the full terminal workspace for the visual part of the demo:

```powershell
phantomguard tui
```

At the `pg>` prompt, use:

```text
status
scan all
scan staged --strict
scan demo/tui-phantom-dependency-demo.py demo/tui-verified-dependency-demo.py demo/tui-unknown-dependency-demo.go
cache status
help
quit
```

`install` is also available in the TUI or CLI. It writes the strict pre-commit hook; use it only in a repository where you want that hook installed.

## 3. Controlled phantom-dependency demonstration

Create a separate Git repository. The long name below is designed to be absent from PyPI; if a future registry entry happens to use it, replace the final suffix with a fresh random value.

```powershell
$pg = Join-Path $sourceRepo 'bin\phantomguard.exe'
$demo = Join-Path $env:TEMP ('phantomguard-hackathon-demo-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $demo | Out-Null
Set-Location $demo
git init
git config user.name 'PhantomGuard Demo'
git config user.email 'demo@example.invalid'

@'
{
  "fail_mode": "warn",
  "languages": ["python"],
  "ignore": [],
  "allow": [],
  "aliases": {},
  "cache_positive_ttl_hours": 168,
  "cache_negative_ttl_hours": 1
}
'@ | Set-Content -NoNewline .phantomguard.json

'import phantomguard_hackathon_probe_20260721_z9x7k3' | Set-Content app.py
git add .
& $pg scan --staged --strict
```

Expected result: a `PHANTOM` finding and exit code `1`. That is the key safety moment for the video: PhantomGuard read the staged Git content, queried the public registry, and blocked a dependency that does not exist.

Then open the TUI and show the same staged result:

```powershell
& $pg tui
```

```text
status
scan staged --strict
```

To demonstrate the guarded fixer, run this TUI command and type `y` only after reading its diff:

```text
fix --file app.py --from phantomguard_hackathon_probe_20260721_z9x7k3 --to requests --ecosystem pypi
```

After the fixer succeeds, stage the corrected file and inspect it again:

```text
quit
```

```powershell
git add app.py
& $pg scan --staged
```

The result should be `EXISTS`, with a visible warning that strict policy still needs integrity-backed provenance. This is intentional: PhantomGuard does not call a package name safe merely because a registry returns `200`.

Return to the PhantomGuard source repository and end the video with its clean strict result:

```powershell
Set-Location $sourceRepo
& .\bin\phantomguard.exe verify --strict
```

## 4. Feature coverage checklist

| Feature | Demo command |
| --- | --- |
| Help and version | `phantomguard --help`, `phantomguard --version` |
| Working-tree scan | `phantomguard scan --all` |
| Exact staged scan | `phantomguard scan --staged --strict` |
| JSON report | `phantomguard scan --staged --json` |
| Full enforcement | `phantomguard verify --strict` |
| TUI | `phantomguard tui` |
| Cache | `phantomguard cache status`; use `phantomguard cache clear` only when you want to reset local results |
| Hook | `phantomguard install` in a chosen Git repository |
| Safe remediation | TUI `fix --file ... --from ... --to ... --ecosystem pypi` |
| Optional AI | `phantomguard ai setup`, then `phantomguard ai explain <confirmed-phantom> --ecosystem pypi` |

The AI setup flow requires your own provider credential and is deliberately outside the TUI, verifier, and hook. Do not put a real token on screen during the demo.
