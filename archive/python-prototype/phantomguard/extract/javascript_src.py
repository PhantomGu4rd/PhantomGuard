"""Conservative regex extraction for JavaScript and TypeScript module specifiers."""

from __future__ import annotations

import json
import re
from importlib.resources import files

from phantomguard.models import Finding

_COMMENT = re.compile(r"/\*.*?\*/|//[^\r\n]*", re.DOTALL)
_IMPORTS = re.compile(
    r"(?:^|[;\n])\s*(?:import\s+(?:[^'\";()]+?\s+from\s+)?|export\s+[^'\";()]+?\s+from\s+|(?:const|let|var)\s+\w+\s*=\s*(?:require|import)\s*\(|(?:require|import)\s*\()\s*['\"]([^'\"\r\n]+)['\"]",
    re.MULTILINE,
)
_SIDE_EFFECT = re.compile(r"(?:^|[;\n])\s*import\s*['\"]([^'\"\r\n]+)['\"]", re.MULTILINE)


def _builtins() -> set[str]:
    """Load Node built-in module names bundled with the package."""
    return set(json.loads(files("phantomguard.data").joinpath("node_builtins.json").read_text()))


def normalize_specifier(specifier: str) -> str | None:
    """Return npm package root for a literal specifier, or None when non-registry."""
    if specifier.startswith((".", "/", "#", "~")):
        return None
    bare = specifier.removeprefix("node:")
    if bare in _builtins():
        return None
    if specifier.startswith("@"):
        parts = specifier.split("/")
        return "/".join(parts[:2]) if len(parts) >= 2 and parts[1] else None
    return specifier.split("/", 1)[0]


def extract(path: str, content: str) -> tuple[list[Finding], int, bool]:
    """Extract statement-position literal JS/TS imports without parsing or execution."""
    stripped = _COMMENT.sub(lambda match: "\n" * match.group(0).count("\n"), content)
    matches = list(_IMPORTS.finditer(stripped)) + list(_SIDE_EFFECT.finditer(stripped))
    seen: set[tuple[str, int]] = set()
    results: list[Finding] = []
    for match in matches:
        name = normalize_specifier(match.group(1))
        line = stripped.count("\n", 0, match.start(1)) + 1
        if name and (name, line) not in seen:
            seen.add((name, line))
            results.append(Finding(name, "npm", path, line))
    return results, 0, False
