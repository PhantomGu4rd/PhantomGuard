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
