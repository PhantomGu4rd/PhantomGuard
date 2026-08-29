# Popular-package data provenance

PhantomGuard uses two embedded popularity lists only for local typosquat-risk matching and suggestions. They are not downloaded, refreshed, or queried during a scan, TUI session, hook run, or CI verification.

| File | Source | Snapshot |
| --- | --- | --- |
| `top_pypi.txt` | [Hugo van Kemenade's Top PyPI Packages feed](https://hugovk.dev/top-pypi-packages/) | The first 1,000 project names, retrieved 2026-07-16. The feed publishes monthly download rankings and exposes more than 1,000 projects. |
| `top_npm.txt` | [npm-rank most-depended-upon list](https://gist.github.com/anvaka/8e8fa57c7ee1350e3491) | 1,000 package names ranked by dependency popularity, rather than download count. |

## Maintenance policy

Treat a dataset refresh as a reviewed source change:

1. Run `go run ./scripts/update_top_packages.go` to fetch and validate a preview.
2. Inspect the upstream source, package-token validity, duplicate count, and resulting diff.
3. Run the same command with `--write` only after that review.

The maintenance utility uses atomic output replacement and is intentionally outside PhantomGuard's runtime path. Do not add a live popularity-data download to the hook or verification workflow.
