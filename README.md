# PhantomGuard

A deterministic, terminal-first dependency verifier and Git pre-commit hook. It statically analyzes Python, JavaScript/TypeScript, and Go files to catch hallucinated or mistyped dependencies before they reach a commit, without executing any code.

**v0.1.3** | Go binary | CLI + TUI + Git pre-commit hook

---

## Team

* **Christos** – Architecture, Provenance Engines, & Release Polish
* **Panagiotis** – Core Backend Engineering & Security Hardening
* **Thanasis** – Architecture Drafting & AI Advisor
* **Vaggelis** – QA, Security Validation, & System Hardening
* **Mishe** – UI Prototyping & Backend Integration

---

## Why it exists

We were inspired to build PhantomGuard out of the need for a definitive, local guardrail that prevents phantom dependencies from ever crossing the commit boundary.

AI-assisted development can introduce package names that look plausible but do not exist. A name that later becomes registered can turn a simple typo into a supply-chain risk. PhantomGuard makes the last local decision before a commit deterministic:

- A registry `404` is a confirmed phantom dependency and blocks the commit.
- Timeouts, DNS failures, and non-definitive HTTP responses are **unknown**, never silently treated as safe.
- A small local typosquat engine highlights names close to popular packages and offers local suggestions.
- Strict mode also blocks incomplete analysis and weak or missing provenance evidence.

PhantomGuard is deliberately narrow. It is not a vulnerability scanner, a package installer, or a general-purpose supply-chain platform.

---

## Architecture

### Tech Stack
* **Languages:** Go, Python, TypeScript, JavaScript, PowerShell
* **Ecosystems:** PyPI, npm, Git, Docker
* **CI/CD:** GitHub Actions

```text
Git index / working-tree selection
             |
             v
static extractors -> local/allowlist filters -> registry/cache validation
             |                                      |
             +-------- provenance + typo-risk ------+
                                                    |
                                                    v
                                       deterministic report and policy
                                                    |
                                    verify/hook, CLI scan, or TUI display
```

The relevant production code is organized as follows:

- `cmd/phantomguard/`: primary CLI, strict verification, hook installation, TUI, AI command, and remediation command wiring.
- `pkg/extractor/`: dependency extraction for supported languages and manifests.
- `pkg/scanner/`: candidate resolution, local filtering, provenance, and policy inputs.
- `pkg/validator/`: bounded public-registry client and concurrent, process-safe cache.
- `pkg/tui/`: terminal workspace and its scanner-backed adapter.
- `pkg/ai/`: optional advisory configuration and provider clients, isolated from enforcement.
- `data/`: embedded aliases and popularity data; see [data/SOURCES.md](data/SOURCES.md).

The archived Python proof of concept lives under `archive/python-prototype/` for historical comparison. It is excluded from the Go build, tests, hook, CI pipeline, and normal verification path.

### Release scope and trust boundaries

The v0.1.3 release is TUI-only. The primary interactive surface is `phantomguard tui`; no web server, browser UI, or frontend asset is shipped.

| Boundary        | What PhantomGuard does                                                                                                                        |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Enforcement     | `phantomguard verify --strict` evaluates supported content from the current Git index. It does not read the working tree to make the verdict. |
| Hook            | `phantomguard install` writes a pre-commit hook that runs `exec phantomguard verify --strict`.                                                |
| Static analysis | Extracts dependency candidates without importing, executing, or evaluating repository code.                                                   |
| Network         | Queries only the public PyPI and npm registries during deterministic verification.                                                            |
| AI              | Disabled by default; it is a separately invoked, user-local advisory feature and never changes an enforcement verdict.                        |
| TUI             | Uses the same scanner, cache, policy, and remediation packages as the CLI. It is not a second security implementation.                        |

`verify` is intentionally broader than a diff: it evaluates all supported files in the current index so an already-indexed dependency cannot evade an enforcement run. Manual `scan --staged` remains a diff-scoped inspection command.

### What deterministic verification checks

PhantomGuard never executes scanned files. It statically inspects:

- Python imports, literal `importlib.import_module()` / `__import__()` calls, and `requirements*.txt`.
- JavaScript and TypeScript imports, `require()`, literal dynamic imports, `package.json` dependency sections, and npm lockfiles.
- Go imports and `go.mod` requirements, using Go's standard parser for source imports.

Before any public registry request, it excludes relative and local imports, standard-library modules, Node built-ins, configured allowlist entries, local Python packages, npm workspaces, TypeScript path aliases, and path/file/git dependencies. Import aliases are normalized using embedded mappings plus the repository's optional alias map.

For PyPI and npm, a `200` means `exists`, a `404` means `phantom`, and every other outcome is `unknown`. Lookups are bounded by a three-second request timeout and an eight-second scan budget. Definitive answers are cached locally; unknown answers are never cached.

The cache defaults to `${XDG_CACHE_HOME:-~/.cache}/phantomguard/cache.json`. Cache writes use a process-level lock and atomic replacement, so concurrent hooks, TUI sessions, and CI runs do not corrupt the file.

#### Index integrity and provenance

Strict enforcement reads the dependency sources, `.phantomguard.json`, and relevant metadata from the Git index. An unstaged configuration or lockfile cannot weaken the decision about staged content.

PhantomGuard also reports dependency provenance when the indexed evidence can prove it:

- Python: hash-pinned `requirements*.txt` entries and matching Pipenv, Poetry, or uv lock records.
- npm: matching `package-lock.json` or `npm-shrinkwrap.json` integrity records.
- Go: a matching staged `go.mod` requirement plus `go.sum` checksum evidence.

PhantomGuard does not query a Go module registry. In strict mode, a Go dependency with matching `go.mod` and `go.sum` evidence is accepted as integrity-backed; missing evidence remains a policy issue. Incomplete analysis also remains visible: strict mode blocks unsupported binary or oversized candidate content, invalid supported source or manifests, unresolved dynamic imports, unknown registry outcomes, and weak provenance. Warn mode still blocks confirmed phantoms and invalid/suspicious package tokens, while surfacing the rest for review.

### Command reference

| Command                                                                     | Purpose                                                                                                                    |
| --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `phantomguard verify [--strict]`                                            | Deterministically evaluate all supported files in the current Git index. This is the enforcement command used by the hook. |
| `phantomguard scan --staged [--strict] [--json]`                            | Inspect only supported files changed in the staged diff.                                                                   |
| `phantomguard scan --all [--strict] [--json]`                               | Inspect supported files in the working tree.                                                                               |
| `phantomguard scan <paths...> [--strict] [--json]`                          | Inspect selected repository-relative working-tree paths.                                                                   |
| `phantomguard tui [--no-color] [--no-interactive]`                          | Start the interactive terminal workspace.                                                                                  |
| `phantomguard install [--force]`                                            | Install the strict pre-commit hook; `--force` backs up and chains a foreign hook.                                          |
| `phantomguard cache` or `phantomguard cache status`                         | Show a privacy-preserving summary of definitive cached results.                                                            |
| `phantomguard cache clear`                                                  | Remove local definitive registry cache entries.                                                                            |
| `phantomguard fix --file <path> --from <name> --to <name> --ecosystem <pypi | npm>`                                                                                                                      | Verify, preview, confirm, apply, and re-check one replacement.            |
| `phantomguard ai setup`                                                     | Interactively configure an optional user-local AI advisor.                                                                 |
| `phantomguard ai explain <package> [--ecosystem pypi                        | npm]`                                                                                                                      | Request an advisory explanation for a matching, confirmed staged phantom. |
| `phantomguard --version`                                                    | Print the binary's embedded release version.                                                                               |

`verify` and `scan` return exit code `1` when policy blocks the result and `2` for invalid invocation, Git, or configuration errors. `PHANTOMGUARD_STRICT=1` forces strict policy. `PHANTOMGUARD_SKIP=1` can skip a manual `scan`, but never `verify` or the installed hook.

### Configuration

Place `.phantomguard.json` at the repository root. The following is the complete supported schema:

```json
{
  "fail_mode": "warn",
  "languages": ["python", "javascript", "go"],
  "ignore": ["vendor/**", "**/migrations/**"],
  "allow": ["internal-utils", "@company/core"],
  "aliases": {
    "internal_sdk": "company-internal-sdk"
  },
  "cache_positive_ttl_hours": 168,
  "cache_negative_ttl_hours": 1
}
```

| Field                      | Meaning                                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------------------- |
| `fail_mode`                | `warn` (default) or `strict`. `strict` blocks unknowns, incomplete analysis, and weak provenance. |
| `languages`                | Enabled language families: `python`, `javascript`, and/or `go`.                                   |
| `ignore`                   | Repository-relative glob patterns for files that should not be scanned.                           |
| `allow`                    | Reviewed local or private package names to exclude before a public lookup.                        |
| `aliases`                  | Import-name to registry-package mapping, merged with PhantomGuard's embedded aliases.             |
| `cache_positive_ttl_hours` | TTL for confirmed registry packages; default `168`.                                               |
| `cache_negative_ttl_hours` | TTL for confirmed phantom packages; default `1`.                                                  |

This repository's committed policy additionally ignores `archive/**` and `demo/**` during automatic scans and enforcement. The demo files intentionally contain fake imports for the TUI walkthrough; production source remains subject to normal deterministic enforcement. Select a demo file explicitly when you want to inspect it, for example `phantomguard scan demo/tui-phantom-dependency-demo.py` or the equivalent TUI command documented in [TUI_GUIDE.md](TUI_GUIDE.md).

The configuration parser rejects unknown fields. AI provider, model, and credential settings are intentionally not allowed in repository configuration.

On Windows, an installed pre-commit hook first uses the official per-user installation at `%LOCALAPPDATA%\\PhantomGuard\\bin\\phantomguard.exe`. This avoids conflicts with stale Python commands named `phantomguard`; other platforms and custom installations fall back to the first `phantomguard` on `PATH`.

### Optional AI advisor

AI is an opt-in advisory convenience, not part of the security decision. Neither `verify`, the Git hook, nor the TUI reads AI configuration or sends data to an AI provider.

If you choose to enable it, run:

```sh
phantomguard ai setup
```

Setup lets you select a supported provider and a model available to your key, then stores the credential only in the user-local `~/.config/phantomguard/ai.json` with owner-only permissions. It never writes credentials to the repository. Supported providers are OpenAI, Anthropic, Google Gemini, xAI, and OpenRouter.

After deterministic verification has found a phantom, ask for a bounded explanation manually:

```sh
phantomguard ai explain reqeusts --ecosystem pypi
```

The command reruns deterministic staged verification and accepts only the matching confirmed phantom. Any suggested replacement must pass an independent registry check and the local typosquat guard. Its output is labeled as advisory and cannot unblock a commit or alter the policy verdict.

### Safe remediation

`phantomguard fix` is intentionally interactive. It requires a clean unstaged working tree, validates the proposed replacement with the target registry, prints a unified diff, requires `y` confirmation, writes the file, and verifies it again. If post-write validation fails, the original content is restored.

Use it after reviewing a deterministic finding, not as a blind automated rewrite.

---

## Installation

### Download a v0.1.3 release

Each GitHub Release contains six self-contained archives and a `checksums.txt` manifest:

- Linux: `phantomguard_v0.1.3_linux_amd64.tar.gz`, `phantomguard_v0.1.3_linux_arm64.tar.gz`
- macOS: `phantomguard_v0.1.3_darwin_amd64.tar.gz`, `phantomguard_v0.1.3_darwin_arm64.tar.gz`
- Windows: `phantomguard_v0.1.3_windows_amd64.zip`, `phantomguard_v0.1.3_windows_arm64.zip`

Download the archive matching your operating system and CPU, verify its SHA-256 entry in `checksums.txt`, and extract it.

On Linux (replace `amd64` with `arm64` when needed):

```sh
tar -xzf phantomguard_v0.1.3_linux_amd64.tar.gz
cd phantomguard_v0.1.3_linux_amd64
sh ./install.sh
phantomguard --version
```

The Linux archive also contains an app-menu launcher and the supplied icon. The installer places them in your user data directory when the archive is used directly; choose **PhantomGuard** from your desktop environment after `phantomguard` is on `PATH`.

On macOS (use `darwin_amd64` for Intel Macs or `darwin_arm64` for Apple
Silicon):

```sh
tar -xzf phantomguard_v0.1.3_darwin_arm64.tar.gz
cd phantomguard_v0.1.3_darwin_arm64
sh ./install.sh
phantomguard --version
```

The macOS archive includes `PhantomGuard.app` with the supplied Finder icon, while the top-level `phantomguard` binary remains the supported CLI and TUI entry point.

On Windows, extract the ZIP and run the installer from PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install.ps1
phantomguard --version
```

The Windows `phantomguard.exe` embeds the supplied application icon.

The Unix installer defaults to `~/.local/bin`; the Windows installer defaults to `%LOCALAPPDATA%\PhantomGuard\bin`. Both locations are user-writable and require no administrator privileges. Open a new terminal if `phantomguard` is not found immediately.

`phantomguard install` refuses to overwrite a foreign pre-commit hook by default. Use `phantomguard install --force` only when you want PhantomGuard to back up and chain that hook. The installed hook deliberately relies on a trusted `phantomguard` binary on `PATH`; it does not execute a repository-controlled binary.

### Quick start

```sh
# Inspect the staged diff while developing.
phantomguard scan --staged --strict

# Install the deterministic pre-commit gate once per repository.
phantomguard install

# Use the terminal workspace for guided local work.
phantomguard tui

# Run the same full-index strict enforcement command used by the hook.
phantomguard verify --strict
```

When a finding is confirmed, PhantomGuard reports its source location, registry, status, provenance signal, and any local typo-risk match. A typical block looks like:

```text
app.py:3  PHANTOM  reqeusts (pypi)
  HIGH-RISK TYPOSQUAT: resembles requests
  did you mean: requests
PhantomGuard blocked this commit.
```

See [DEMO_WORKFLOW.md](DEMO_WORKFLOW.md) for a complete hackathon-video script, including a disposable phantom-dependency example and every supported command surface.

### Terminal workspace (TUI)

Run `phantomguard tui` from a Git repository to open the interactive terminal workspace. It adapts to the terminal width, disables ANSI styling for redirected output, `NO_COLOR`, or `TERM=dumb`, and can render its welcome screen without a prompt:

```sh
phantomguard tui --no-color
phantomguard tui --no-interactive
```

Inside the TUI, use `help` to see commands. The most useful flow is:

```text
pg> status
pg> scan staged --strict
pg> cache
pg> fix --file app.py --from reqeusts --to requests --ecosystem pypi
pg> quit
```

See [TUI_GUIDE.md](TUI_GUIDE.md) for the full command reference, scan scopes, output behavior, and remediation workflow.

---

## Build from source

Prerequisites: Go 1.21 or later and Git.

```sh
go test ./...
go build -trimpath -o bin/phantomguard ./cmd/phantomguard
export PATH="$PWD/bin:$PATH"
phantomguard --version
phantomguard install
```

For a Windows source build:

```powershell
.\scripts\prepare-dev.ps1
Get-Command phantomguard -All
phantomguard --version
phantomguard install
```

`prepare-dev.ps1` builds `bin\phantomguard.exe` and moves that directory to the front of the current PowerShell session's `PATH`. This prevents an unrelated Python package or older executable named `phantomguard` from being selected first. For a persistent installation, run `installer\install.ps1 -SourcePath .\bin\phantomguard.exe`; it puts PhantomGuard first in your user `PATH` as well.

### Development and release process

The project uses Go's standard library at runtime. From a source checkout:

```sh 
go test ./...
go vet ./...
make check
make release
```

`make release` packages six statically built binaries for Linux, macOS, and Windows on amd64 and arm64, injects the release version into each binary, and writes SHA-256 values to `dist/checksums.txt`. The release workflow tests source, verifies the checksum manifest, and publishes only the explicit artifacts for the pushed `v*` tag. Installer verification runs on Linux, Windows, and macOS.

For a complete local release check, also run:

```sh
CGO_ENABLED=1 go test -race ./...
docker build -t phantomguard:local .
```

The Docker image contains the binary, Git, and CA certificates so `phantomguard verify --strict` can run in a repository-mounted container.

---

## Limitations

- v0.1.3 does not parse Rust, Java, C/C++, notebooks, Deno/URL imports, or HTML `<script>` tags.
- Static JavaScript and Python extraction deliberately does not resolve arbitrary computed imports. Strict mode blocks unresolved dynamic imports; warn mode reports them.
- PyPI and npm are the only live registry validators. Go is assessed through static extraction and staged checksum provenance, not a Go registry query.
- Private registries and authenticated feeds are out of scope. Review internal names and place them in `allow` when appropriate.
- A developer can bypass a local hook with `git commit --no-verify`. Run `phantomguard verify --strict` in CI as the project-level backstop.
- Typosquat suggestions come from the embedded top-1,000 PyPI and npm datasets. Review source provenance and diffs before refreshing them; do not download popularity data during a hook run.

### Roadmap

* Expand static analysis to **Rust (Cargo)**, **Java (Maven/Gradle)**, and **C/C++ (CMake/Conan)**.
* Enhance extraction algorithms for complex multi-package monorepos and private workspace setups. 

---

## License

PhantomGuard is released under the PolyForm Noncommercial License 1.0.0. Modifying and sharing this code is encouraged for non-commercial use with attribution.
