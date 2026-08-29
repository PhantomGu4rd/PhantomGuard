"""Refresh bundled package suggestion lists from public registry popularity feeds.

This maintenance script is intentionally not part of runtime or tests. Review its
generated output before committing so the offline data remains curated.
"""

from __future__ import annotations

import argparse
import urllib.request
from pathlib import Path


def fetch_lines(url: str) -> list[str]:
    """Fetch a newline-delimited public package list."""
    with urllib.request.urlopen(url, timeout=15) as response:  # noqa: S310
        return [
            line.strip() for line in response.read().decode("utf-8").splitlines() if line.strip()
        ]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--pypi-url", required=True, help="reviewed public newline-delimited PyPI ranking URL"
    )
    parser.add_argument(
        "--npm-url", required=True, help="reviewed public newline-delimited npm ranking URL"
    )
    parser.add_argument("--limit", type=int, default=500)
    args = parser.parse_args()
    destination = Path(__file__).parents[1] / "phantomguard" / "data"
    for url, target in ((args.pypi_url, "top_pypi.txt"), (args.npm_url, "top_npm.txt")):
        values = fetch_lines(url)[: args.limit]
        (destination / target).write_text("\n".join(values) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
