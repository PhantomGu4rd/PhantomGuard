"""Verdict policy and exit semantics."""

from __future__ import annotations


def blocks(status: str, fail_mode: str) -> bool:
    """Return whether a status must block under the active mode."""
    if status in {"phantom", "suspicious", "denied"}:
        return True
    return status == "unknown" and fail_mode == "strict"


def exit_code(statuses: list[str], fail_mode: str) -> int:
    """Return the scan exit code: one for policy blocks, zero otherwise."""
    return 1 if any(blocks(status, fail_mode) for status in statuses) else 0
