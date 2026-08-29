"""Shared immutable data structures used by PhantomGuard."""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(frozen=True)
class Finding:
    """A dependency reference discovered in staged or supplied content."""

    name: str
    ecosystem: str
    file: str
    line: int
    confidence: str = "high"


@dataclass(frozen=True)
class Result:
    """Registry or policy result associated with a discovered finding."""

    finding: Finding
    package: str
    status: str
    suggestions: tuple[str, ...] = ()
    detail: str = ""


@dataclass
class ScanReport:
    """Complete scan report suitable for human or JSON output."""

    results: list[Result] = field(default_factory=list)
    notices: list[str] = field(default_factory=list)
    unscannable: int = 0
    files_scanned: int = 0
