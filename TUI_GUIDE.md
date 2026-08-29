# PhantomGuard terminal workspace

`phantomguard tui` is PhantomGuard's interactive, terminal-only workspace. It is a convenience layer over the same deterministic scanner used by the command-line interface and Git hook; it does not start a local server, open a browser, execute repository code, or invoke AI.

Run it from a Git repository:

```sh
phantomguard tui
```

The welcome screen shows the repository, current branch, active policy, and build metadata. Type `help` at the `pg>` prompt for the command list.

## Display behavior

The TUI is designed to remain useful in plain terminals and automation contexts:

- It adapts its layout to the terminal width.
- ANSI color is disabled automatically for redirected output, `NO_COLOR`, and `TERM=dumb`.
- `--no-color` disables ANSI output explicitly.
- `--no-interactive` renders the welcome screen and exits, which is useful for screenshots and smoke checks.

```sh
phantomguard tui --no-color
phantomguard tui --no-interactive
```

If standard input or output is not a terminal, PhantomGuard renders the welcome screen instead of waiting for a command prompt.

## Commands

| Command | What it does |
| --- | --- |
| `help` | Show the available terminal commands. |
| `status` or `policy` | Show the repository, branch, and configured fail mode. |
| `scan` or `scan staged` | Scan the staged diff using Git-index content. |
| `scan all` | Scan all supported working-tree files. |
| `scan <path> [<path> ...]` | Scan selected repository-relative working-tree paths. Explicit paths override automatic ignore patterns, so intentional demo fixtures remain testable. Quote a path containing spaces. |
| `scan ... --strict` | Apply strict policy to that scan. |
| `cache` or `cache status` | Show aggregate local cache information without revealing package names or cache paths. |
| `cache clear` | Remove cached definitive registry answers. |
| `install` | Install the strict pre-commit hook. |
| `install --force` | Back up and chain a foreign hook, then install PhantomGuard. |
| `fix --file <path> --from <name> --to <name> --ecosystem <pypi|npm>` | Validate, preview, confirm, and re-check one dependency replacement. |
| `version` or `about` | Show the embedded release version and build metadata. |
| `quit` or `exit` | Close the workspace. |

The line parser supports quoted arguments and does not perform shell expansion or execute commands.

## Recommended workflow

Use the terminal workspace to inspect and repair a change, then use `verify` as the final enforcement decision:

```text
pg> status
pg> scan staged --strict
pg> fix --file app.py --from reqeusts --to requests --ecosystem pypi
pg> scan staged --strict
pg> quit
```

Then stage the corrected file and run:

```sh
phantomguard verify --strict
```

The TUI's staged scan is deliberately diff-scoped. In contrast, `verify` evaluates all supported files in the current Git index and is the command used by the pre-commit hook.

## Scanning this repository's demo fixtures

The committed policy excludes `demo/**` from automatic `scan all`, staged scans, and `verify` because those files intentionally contain phantom and unknown imports. That exclusion is reported as a notice instead of silently appearing as a clean scan. To exercise the fixtures in the TUI, select them explicitly:

```text
pg> scan demo/tui-phantom-dependency-demo.py demo/tui-verified-dependency-demo.py demo/tui-unknown-dependency-demo.go
```

This produces phantom, existing, and unknown results without weakening the repository's normal pre-commit enforcement.

## Scan results and policy

TUI scan results identify each candidate by file and line, status, resolved package, and registry. They also surface local typo-risk matches, suggestions, provenance evidence, analysis notices, and unresolved dynamic imports.

- `PHANTOM` and `SUSPICIOUS` findings block under both `warn` and `strict` policy.
- `UNKNOWN`, parser or dynamic-import incomplete analysis, and weak provenance are visible in `warn` mode and block in `strict` mode.
- PyPI and npm receive live public-registry checks. Go dependencies are not sent to a Go registry; matching staged `go.mod` and `go.sum` evidence is accepted as integrity-backed in strict mode.

`verify --strict` is the stronger final gate: in addition to its full-index scope, it treats skipped binary or oversized candidate content as incomplete analysis.

`PHANTOMGUARD_SKIP=1` skips manual scans, including scans launched from the TUI. It never skips `phantomguard verify` or the installed pre-commit hook.

## The fixer

The `fix` command is intentionally not an auto-fixer. It requires a clean unstaged working tree, validates the replacement against the selected registry, shows a unified diff, waits for a `y` confirmation, writes only the requested file, and verifies the result again. If post-write validation fails, it restores the original file.

Review the diff and the proposed package before confirming. Use the final `verify --strict` command after staging the correction.

## AI stays outside the TUI

The TUI does not load AI credentials or make AI requests. If you explicitly configure the optional advisor with `phantomguard ai setup`, invoke it separately after a deterministic phantom finding:

```sh
phantomguard ai explain reqeusts --ecosystem pypi
```

That advisory is independently registry-checked and can never change a TUI scan result, a `verify` verdict, or a Git-hook decision.
