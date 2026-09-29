# PhantomGuard design decisions

This file records the security and product decisions that shape the v0.1.3 release.

## Product surface

- PhantomGuard is terminal-first and TUI-only. The browser dashboard and frontend were removed so the release has one functional user interface and one primary binary.
- The TUI is an adapter over the production scanner, cache, policy, hook installer, and remediation packages. It does not duplicate or reinterpret enforcement logic.
- The TUI is intentionally separate from the hook decision: it supports guided inspection and remediation, while `verify --strict` remains the final deterministic enforcement command.

## Deterministic enforcement

- `verify` reads all supported content in the current Git index, not only the staged diff. This prevents already-indexed dependencies from escaping a subsequent enforcement run.
- Manual `scan --staged` remains diff-scoped because it is an inspection command; `scan --all` and selected-path scans deliberately inspect working-tree content.
- The indexed `.phantomguard.json`, manifests, lockfiles, workspace metadata, and TypeScript paths are used for a staged verdict. An unstaged local edit cannot exempt a staged dependency or supply stronger provenance.
- Scanned code is never executed, imported, evaluated, or passed to a package manager. Candidate extraction is static.
- Public registry outcomes are intentionally three-valued: only `200` means `exists`, only `404` means `phantom`, and every other response is `unknown`.
- Invalid registry tokens are `suspicious` and block locally before a URL is constructed. Untrusted package names are never interpolated unchecked into a registry request.
- Strict policy blocks confirmed phantoms, suspicious names, unknown outcomes, weak provenance, and incomplete analysis. Warn policy still blocks phantoms and suspicious names while making the remaining uncertainty visible.
- Incomplete analysis is a first-class result: oversized or binary candidate content, invalid supported source or manifests, and unresolved dynamic imports are not silently ignored. Strict mode blocks them.
- The repository's `demo/**` fixtures are explicitly ignored because they intentionally contain fake dependencies for TUI demonstrations; this exception is visible in the committed policy rather than hidden in scanner logic.

## Ecosystem and provenance scope

- PyPI and npm are the only live registry validators in v0.1.3. Requests use a bounded timeout and scan-wide budget so a slow network cannot be mistaken for a safe result.
- Python and npm provenance is derived from staged requirement, lockfile, and integrity evidence. Missing evidence is reported explicitly rather than inferred.
- Go source is parsed with the native Go parser. PhantomGuard does not query a Go module registry; a matching staged `go.mod` requirement and `go.sum` checksum are accepted as integrity-backed evidence in strict policy.
- Local Python packages, npm workspaces, TypeScript path aliases, standard libraries, Node built-ins, reviewed allowlist entries, and path/file/git dependencies are filtered before any public lookup.
- The embedded popularity datasets are local, versioned, and limited to 1,000 PyPI plus 1,000 npm names. Maintainers must review source, token validity, duplicates, and diffs before updating them; a hook must never download those datasets.

## Cache and resilience

- Definitive `exists` and `phantom` responses are cached; `unknown` results are never cached.
- A cache read treats malformed JSON as a cache miss and preserves the bad file for diagnosis. Cache corruption must not stop an enforcement run.
- Cache writes use an operating-system file lock, reload the authoritative snapshot while locked, and atomically replace the JSON file. Concurrent hook, TUI, and CI processes cannot silently lose each other's entries or leave a partial cache.
- Local fallback suggestions are capped and validate limits defensively so an unexpected caller cannot flood output or trigger invalid slice operations.

## Optional AI advisor

- AI is a manual advisory plane. `verify`, the Git hook, and the TUI never load AI configuration or call a provider.
- `ai explain` accepts only a matching, confirmed staged PyPI or npm phantom after rerunning deterministic verification. It cannot change a verdict or unblock a commit.
- AI provider, model, and credential configuration live only in user-local `~/.config/phantomguard/ai.json` with owner-only permissions. Repository configuration rejects AI fields.
- Providers and endpoints are allowlisted. Every AI-suggested package is independently validated against the correct registry and the local typosquat policy before display.

## Hook, release, and testing

- The installed pre-commit hook uses `exec phantomguard verify --strict` and requires a trusted installed binary on `PATH`. It does not execute a repository-controlled executable.
- `PHANTOMGUARD_SKIP=1` may skip manual scans but never `verify` or the installed hook. `PHANTOMGUARD_STRICT=1` forces strict policy for supported commands.
- Release builds inject the tag into a shared build-information variable and package the same primary binary for Linux, macOS, and Windows on amd64 and arm64.
- Release icons use each platform's native mechanism: a checked-in Windows executable resource, a macOS `PhantomGuard.app` bundle with an ICNS asset, and a Linux desktop launcher with a PNG icon. The CLI binary remains available in every archive.
- Release publishing enumerates the expected artifacts instead of globbing a directory, preventing a stale build artifact from being attached to a release.
- CI checks formatting, static analysis, unit and integration tests, the race detector, platform archives, installers on Linux/Windows/macOS, and the Docker build.

## Security audit

### Data exposure boundaries

- ✅ **Repository configuration (``.phantomguard.json`) never contains credentials.** Supported fields are fail mode, language selection, allowlist, ignore patterns, custom aliases, and cache TTL.
- ✅ **API keys are never sent to Git.** AI credentials live only in user-local `~/.config/phantomguard/ai.json` (Unix) or `%APPDATA%\phantomguard\ai.json` (Windows), marked as user-read-only.
- ✅ **Credential files are actively skipped during scanning.** The scanner rejects `.env*`, `.aws/`, `.ssh/`, `.npmrc`, `.dockercfg`, `secrets.yml`, certificate files (`*.pem`, `*.crt`), SSH keys (`id_rsa*`, etc.), and other known secret patterns before they can be read or analyzed.
- ✅ **`verify`, hooks, and TUI never contact external services beyond PyPI and npm registries.** No telemetry, analytics, or external validation occurs outside explicitly invoked optional commands.
- ✅ **Only `ai explain` uses external AI providers,** and only when manually invoked after deterministic verification finds a confirmed phantom.

### Third-party and external call safety

- ✅ **All HTTP clients enforce strict timeouts.** Registry validation uses 3-second request timeout + 8-second scan budget; AI calls use 10-second timeout.
- ✅ **Endpoints are allowlisted.** AI providers and registry URLs are hardcoded; no environment variable or configuration file can redirect network traffic to an attacker-controlled server.
- ✅ **Package names are validated before interpolation.** All candidate names match the `[A-Za-z0-9._:/-]*` pattern; underscore and dash normalization follows package registry rules, not user input.
- ✅ **DNS failures, connection timeouts, and non-definitive HTTP responses default to `unknown`, never silently treated as safe.** Conservative unknown-blocking in strict mode prevents network degradation from weakening policy.

### Local and staging-only verification

- ✅ **No repository-controlled code executes.** The Git hook invokes a trusted `phantomguard` binary on `PATH`, not a repository-controlled script.
- ✅ **Staged-only verification prevents working-tree manipulation.** The `verify` command reads configuration and source code exclusively from the Git index, ensuring unstaged changes cannot weaken enforcement.
- ✅ **Cache corruption is not fatal.** Malformed cache entries are treated as cache misses; the file is preserved for diagnosis and a fresh lookup is performed.
- ✅ **Configuration tampering is detected.** Strict mode enforces strong provenance evidence from the Git index, so an unstaged edit to `.phantomguard.json` or a manifest cannot retroactively exempt a staged dependency.

### Incident response and transparency

- ✅ **All skipped files are reported with explicit reasons.** Users know when a file exceeds size limits, contains binary content, matches credential patterns, or is ignored by policy.
- ✅ **Analysis incompleteness is a first-class result.** Oversized files, unresolved dynamic imports, and binary content block commits in strict mode rather than being silently excluded.
- ✅ **Upstream vulnerabilities cannot alter local policy.** PhantomGuard does not query vulnerability databases, SBOM endpoints, or trusted supply-chain platforms; it only checks whether a package name exists in the target registry.

## Release automation

- **GoReleaser** is the standard Go release tool and replaces the custom `scripts/release-package/` script. Configuration lives in `.goreleaser.yml` and is invoked via GitHub Actions without custom Go code.
- Release builds are triggered by Git tags (e.g., `git tag v1.0.0 && git push --tags`). The CI workflow tests, builds, packages, checksums, and publishes all six platform binaries automatically.
- Archives include README.md, TUI_GUIDE.md, LICENSE, DEMO_WORKFLOW.md, and DECISIONS.md. Windows archives are `.zip`; Unix archives are `.tar.gz` with owner-executable permissions on binaries.
- Archive checksums are generated and published alongside binaries. The `--generate-notes` flag auto-generates GitHub release notes from commit history between tags.
- Local development can use `make release-local` to run the legacy `scripts/release-package/` script for backward compatibility, but production releases use GoReleaser.
- Platform-specific desktop assets (Linux `.desktop` launcher, macOS `.app` bundle) require separate CI steps for full implementation and can be added in post-processing if needed.


