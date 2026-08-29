"""Import resolution and repository-local false-positive filters."""

from __future__ import annotations

import configparser
import json
import re
import sys
import tomllib
from fnmatch import fnmatch
from importlib.resources import files
from pathlib import Path

from phantomguard.config import Config
from phantomguard.models import Finding

_PEP503 = re.compile(r"[-_.]+")


def normalize_pypi(name: str) -> str:
    """Normalize a PyPI distribution name according to PEP 503."""
    return _PEP503.sub("-", name).lower()


def aliases(config: Config) -> dict[str, str]:
    """Return bundled aliases extended (and overridden) by user configuration."""
    bundled = json.loads(files("phantomguard.data").joinpath("aliases.json").read_text())
    bundled.update(config.aliases)
    return {str(key): str(value) for key, value in bundled.items()}


def resolve_name(finding: Finding, config: Config) -> str:
    """Resolve an imported module to its public registry package name."""
    name = aliases(config).get(finding.name, finding.name)
    return normalize_pypi(name) if finding.ecosystem == "pypi" else name.lower()


def ignored(path: str, config: Config) -> bool:
    """Return whether a repository-relative path matches a configured ignore glob."""
    normalized = path.replace("\\", "/")
    return any(
        fnmatch(normalized, pattern) or Path(normalized).match(pattern) for pattern in config.ignore
    )



def _project_python_names(root: Path) -> set[str]:
    names: set[str] = set()
    pyproject = root / "pyproject.toml"
    if pyproject.is_file():
        try:
            project = tomllib.loads(pyproject.read_text(encoding="utf-8")).get("project", {})
            if isinstance(project, dict) and isinstance(project.get("name"), str):
                names.add(project["name"].replace("-", "_"))
        except (OSError, tomllib.TOMLDecodeError):
            pass
    setup_cfg = root / "setup.cfg"
    if setup_cfg.is_file():
        parser = configparser.ConfigParser()
        try:
            parser.read(setup_cfg, encoding="utf-8")
            if parser.has_option("metadata", "name"):
                names.add(parser.get("metadata", "name").replace("-", "_"))
        except (OSError, configparser.Error):
            pass
    return names


def _local_python(root: Path, name: str) -> bool:
    for base in (root, root / "src"):
        if (base / f"{name}.py").is_file() or (base / name / "__init__.py").is_file():
            return True
    return name in _project_python_names(root)


def _workspace_patterns(root: Path) -> list[str]:
    patterns: list[str] = []
    package = root / "package.json"
    if package.is_file():
        try:
            workspaces = json.loads(package.read_text(encoding="utf-8")).get("workspaces", [])
            if isinstance(workspaces, dict):
                workspaces = workspaces.get("packages", [])
            if isinstance(workspaces, list):
                patterns.extend(str(item) for item in workspaces)
        except (OSError, json.JSONDecodeError):
            pass
    pnpm = root / "pnpm-workspace.yaml"
    if pnpm.is_file():
        try:
            for line in pnpm.read_text(encoding="utf-8").splitlines():
                match = re.match(r"\s*-\s*['\"]?([^'\"#]+)", line)
                if match:
                    patterns.append(match.group(1).strip())
        except OSError:
            pass
    return patterns


def _workspace_names(root: Path) -> set[str]:
    names: set[str] = set()
    for pattern in _workspace_patterns(root):
        for manifest in root.glob(f"{pattern.rstrip('/')}/package.json"):
            try:
                name = json.loads(manifest.read_text(encoding="utf-8")).get("name")
                if isinstance(name, str):
                    names.add(name.lower())
            except (OSError, json.JSONDecodeError):
                pass
    return names


def _ts_aliases(root: Path) -> set[str]:
    config = root / "tsconfig.json"
    if not config.is_file():
        return set()
    try:
        paths = (
            json.loads(config.read_text(encoding="utf-8"))
            .get("compilerOptions", {})
            .get("paths", {})
        )
        return (
            {str(key).rstrip("/*").lower() for key in paths} if isinstance(paths, dict) else set()
        )
    except (OSError, json.JSONDecodeError, AttributeError):
        return set()


def is_local_or_allowed(root: Path, finding: Finding, package: str, config: Config) -> bool:
    """Return whether a reference is stdlib, local/workspace, or explicitly allowed."""
    candidates = {finding.name, package, normalize_pypi(finding.name), normalize_pypi(package)}
    if candidates & set(config.allow):
        return True
    if finding.ecosystem == "pypi":
        return finding.name in sys.stdlib_module_names or _local_python(root, finding.name)
    return finding.name.lower() in _workspace_names(root) or finding.name.lower() in _ts_aliases(
        root
    )
