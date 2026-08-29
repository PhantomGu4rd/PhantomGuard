"""Human and machine-readable scan reporting."""

from __future__ import annotations

import json
import sys

from phantomguard.models import ScanReport
from phantomguard.policy import exit_code


def _color(text: str, code: str) -> str:
    return f"\033[{code}m{text}\033[0m" if sys.stdout.isatty() else text


def render(report: ScanReport, fail_mode: str, json_output: bool = False) -> tuple[str, int]:
    """Render a report and determine its policy exit code."""
    code = exit_code([result.status for result in report.results], fail_mode)
    if json_output:
        payload = {
            "findings": [
                {
                    "ecosystem": item.finding.ecosystem,
                    "name": item.package,
                    "file": item.finding.file,
                    "line": item.finding.line,
                    "confidence": item.finding.confidence,
                    "verdict": item.status,
                    "suggestions": list(item.suggestions),
                    "detail": item.detail,
                }
                for item in report.results
            ],
            "summary": {
                "files_scanned": report.files_scanned,
                "unscannable": report.unscannable,
                "notices": report.notices,
            },
            "exit_code": code,
        }
        return json.dumps(payload, indent=2, sort_keys=True), code
    lines = list(report.notices)
    problematic = [result for result in report.results if result.status != "exists"]
    if not problematic:
        lines.append(
            _color(
                f"[OK] PhantomGuard scanned {report.files_scanned} file(s): "
                "no phantom dependencies found",
                "32",
            )
        )
    else:
        label = "blocked this commit" if code else "reported findings"
        lines.append(
            _color(
                f"{'[BLOCKED]' if code else '[WARN]'} PhantomGuard {label}", "31" if code else "33"
            )
        )
        for result in problematic:
            verdict = {
                "phantom": "NOT FOUND",
                "unknown": "UNVERIFIED",
                "suspicious": "SUSPICIOUS",
                "denied": "DENYLISTED",
            }.get(result.status, result.status.upper())
            lines.append(
                f"  {result.finding.ecosystem:<5} {result.package:<28} "
                f"{result.finding.file}:{result.finding.line}  {verdict}"
            )
            if result.suggestions:
                lines.append(f"        did you mean: {', '.join(result.suggestions[:3])}?")
        if any(item.status == "unknown" for item in problematic) and fail_mode == "warn":
            lines.append(
                "  Network uncertainty is allowed only because fail_mode=warn; use --strict in CI."
            )
    if report.unscannable:
        lines.append(f"  {report.unscannable} dynamic import(s) could not be statically scanned.")
    return "\n".join(lines), code
