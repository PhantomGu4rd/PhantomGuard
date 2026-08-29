"""End-to-end static scanning pipeline shared by hooks, CI, and tests."""

from __future__ import annotations

import difflib
import json
from importlib.resources import files
from pathlib import Path

from phantomguard.cache import Cache
from phantomguard.config import Config
from phantomguard.extract import javascript_src, manifests, python_src
from phantomguard.models import Finding, Result, ScanReport
from phantomguard.registry import RegistryClient
from phantomguard.resolve import is_local_or_allowed, resolve_name

_PYTHON = {".py"}
_JAVASCRIPT = {".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"}


def relevant(path: str) -> bool:
    """Return whether a path is a source or manifest file supported in v1."""
    name = Path(path).name.lower()
    return (
        Path(path).suffix.lower() in _PYTHON | _JAVASCRIPT
        or name in {"pyproject.toml", "package.json"}
        or (name.startswith("requirements") and name.endswith(".txt"))
    )


def extract_one(path: str, content: str) -> tuple[list[Finding], list[str], int]:
    """Extract all findings from one recognized file."""
    suffix = Path(path).suffix.lower()
    if suffix in _PYTHON:
        findings, unscannable, fallback = python_src.extract(path, content)
        return (
            findings,
            (
                [f"{path}: syntax error; scanned imports with reduced confidence"]
                if fallback
                else []
            ),
            unscannable,
        )
    if suffix in _JAVASCRIPT:
        findings, unscannable, _ = javascript_src.extract(path, content)
        return findings, [], unscannable
    findings, notices = manifests.extract(path, content)
    return findings, notices, 0


def _project_names(root: Path) -> set[str]:
    names: set[str] = set()
    for file in (root / "package-lock.json", root / "poetry.lock", root / "uv.lock"):
        if not file.is_file():
            continue
        try:
            if file.name == "package-lock.json":
                data = json.loads(file.read_text(encoding="utf-8"))
                packages = data.get("packages", {}) if isinstance(data, dict) else {}
                for value in packages.values() if isinstance(packages, dict) else ():
                    if isinstance(value, dict) and isinstance(value.get("name"), str):
                        names.add(value["name"])
                dependencies = data.get("dependencies", {}) if isinstance(data, dict) else {}
                if isinstance(dependencies, dict):
                    names.update(str(key) for key in dependencies)
            else:
                for line in file.read_text(encoding="utf-8", errors="replace").splitlines():
                    if line.startswith("name = "):
                        names.add(line.split("=", 1)[1].strip().strip('"'))
        except (OSError, json.JSONDecodeError):
            continue
    return names


def suggestions(root: Path, ecosystem: str, name: str) -> tuple[str, ...]:
    """Suggest close known public or project package names, without network calls."""
    resource = "top_pypi.txt" if ecosystem == "pypi" else "top_npm.txt"
    known = files("phantomguard.data").joinpath(resource).read_text().splitlines()
    known.extend(_project_names(root))
    return tuple(difflib.get_close_matches(name, sorted(set(known)), n=3, cutoff=0.6))


def scan_contents(
    root: Path,
    contents: dict[str, str],
    config: Config,
    client: RegistryClient | None = None,
    cache: Cache | None = None,
) -> ScanReport:
    """Scan supplied content mapping; callers decide Git-index versus working-tree reads."""
    client = client or RegistryClient()
    cache = cache or Cache()
    report = ScanReport(files_scanned=len(contents))
    extracted: list[Finding] = []
    for path, content in contents.items():
        findings, notices, unscannable = extract_one(path, content)
        extracted.extend(findings)
        report.notices.extend(notices)
        report.unscannable += unscannable
    pending: set[tuple[str, str]] = set()
    active: list[tuple[Finding, str, str]] = []
    for finding in extracted:
        package = resolve_name(finding, config)
        if is_local_or_allowed(root, finding, package, config):
            continue
        if {finding.name, package} & set(config.deny):
            active.append((finding, package, "denied"))
            continue
        cached = cache.get(
            finding.ecosystem, package, config.positive_ttl_hours, config.negative_ttl_hours
        )
        if cached:
            active.append((finding, package, cached))
        else:
            active.append((finding, package, "pending"))
            pending.add((finding.ecosystem, package))
    statuses = client.lookup_many(pending) if pending else {}
    for ecosystem, package in pending:
        status = statuses[(ecosystem, package)]
        cache.put(ecosystem, package, status)
    for finding, package, status in active:
        if status == "pending":
            status = statuses[(finding.ecosystem, package)]
        report.results.append(
            Result(
                finding,
                package,
                status,
                suggestions(root, finding.ecosystem, package) if status != "exists" else (),
            )
        )
    return report


def working_tree_contents(
    root: Path, paths: list[str] | None = None
) -> tuple[dict[str, str], list[str]]:
    """Read relevant working-tree files for manual/CI scan mode."""
    selected = [root / path for path in paths] if paths else list(root.rglob("*"))
    contents: dict[str, str] = {}
    notices: list[str] = []
    for file in selected:
        if not file.is_file():
            continue
        relative = file.relative_to(root).as_posix()
        if not relevant(relative):
            continue
        try:
            raw = file.read_bytes()
            if len(raw) > 1_048_576:
                notices.append(f"{relative}: skipped (>1 MiB)")
                continue
            try:
                contents[relative] = raw.decode("utf-8")
            except UnicodeDecodeError:
                contents[relative] = raw.decode("latin-1")
        except OSError as error:
            notices.append(f"{relative}: skipped ({error})")
    return contents, notices
