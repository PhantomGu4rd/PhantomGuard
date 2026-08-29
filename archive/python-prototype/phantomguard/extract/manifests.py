"""Dependency extraction from Python and Node manifests."""

from __future__ import annotations

import json
import re
import tomllib

from phantomguard.models import Finding

_REQ_NAME = re.compile(r"^([A-Za-z0-9][A-Za-z0-9_.-]*)")
_NON_REGISTRY = ("file:", "link:", "workspace:", "git+", "github:", "http://", "https://")


def _python_requirement(value: str) -> str | None:
    """Return a normalized package token from a requirement declaration."""
    value = value.split("#", 1)[0].split(";", 1)[0].strip()
    if not value or value.startswith("-") or "://" in value or value.startswith((".", "/")):
        return None
    match = _REQ_NAME.match(value)
    if not match:
        return None
    return match.group(1)


def requirements(path: str, content: str) -> tuple[list[Finding], list[str]]:
    """Extract PyPI requirements while excluding options, VCS URLs and local paths."""
    findings: list[Finding] = []
    notices: list[str] = []
    for line_no, raw in enumerate(content.splitlines(), 1):
        name = _python_requirement(raw)
        if name:
            findings.append(Finding(name, "pypi", path, line_no))
    return findings, notices


def pyproject(path: str, content: str) -> tuple[list[Finding], list[str]]:
    """Extract PEP 621 project and optional dependencies from pyproject TOML."""
    try:
        data = tomllib.loads(content)
    except tomllib.TOMLDecodeError as error:
        return [], [f"{path}: invalid pyproject.toml skipped ({error})"]
    project = data.get("project", {})
    if not isinstance(project, dict):
        return [], []
    values = list(project.get("dependencies", []) or [])
    optional = project.get("optional-dependencies", {})
    if isinstance(optional, dict):
        for group in optional.values():
            if isinstance(group, list):
                values.extend(group)
    lines = content.splitlines()
    findings: list[Finding] = []
    for value in values:
        if isinstance(value, str) and (name := _python_requirement(value)):
            line = next((i for i, item in enumerate(lines, 1) if value in item), 1)
            findings.append(Finding(name, "pypi", path, line))
    return findings, []


def _registry_version(value: object) -> bool:
    """Whether a package.json version specifier targets the npm registry."""
    return (
        isinstance(value, str)
        and bool(value.strip())
        and not value.strip().startswith(_NON_REGISTRY)
    )


def package_json(path: str, content: str) -> tuple[list[Finding], list[str]]:
    """Extract dependency keys whose version specs refer to the npm registry."""
    try:
        data = json.loads(content)
    except json.JSONDecodeError as error:
        return [], [f"{path}: invalid package.json skipped ({error.msg})"]
    if not isinstance(data, dict):
        return [], []
    findings: list[Finding] = []
    for section in ("dependencies", "devDependencies", "peerDependencies", "optionalDependencies"):
        values = data.get(section, {})
        if not isinstance(values, dict):
            continue
        for name, version in values.items():
            if isinstance(name, str) and _registry_version(version):
                line = next(
                    (i for i, item in enumerate(content.splitlines(), 1) if f'"{name}"' in item), 1
                )
                findings.append(Finding(name, "npm", path, line))
    return findings, []


def extract(path: str, content: str) -> tuple[list[Finding], list[str]]:
    """Dispatch manifest extraction according to its recognized filename."""
    lower = path.lower()
    filename = lower.rsplit("/", 1)[-1]
    if filename.startswith("requirements") and filename.endswith(".txt"):
        return requirements(path, content)
    if filename == "pyproject.toml":
        return pyproject(path, content)
    if filename == "package.json":
        return package_json(path, content)
    return [], []
