"""Safe reads from Git's index; scanning never consults the working tree."""

from __future__ import annotations

import subprocess
from pathlib import Path


class GitError(RuntimeError):
    """Raised when Git data cannot be safely read."""


def repo_root(cwd: Path) -> Path:
    """Return the current repository root or raise a concise error."""
    completed = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"], cwd=cwd, capture_output=True
    )
    if completed.returncode:
        raise GitError("not inside a Git repository")
    return Path(completed.stdout.decode("utf-8", errors="surrogateescape").strip())


def staged_files(root: Path) -> list[str]:
    """Return added/copied/modified/renamed file paths from the index."""
    completed = subprocess.run(
        ["git", "diff", "--cached", "--name-only", "--diff-filter=ACMR"],
        cwd=root,
        capture_output=True,
    )
    if completed.returncode:
        detail = completed.stderr.decode("utf-8", errors="replace").strip()
        raise GitError(detail or "could not list staged files")
    output = completed.stdout.decode("utf-8", errors="surrogateescape")
    return [line for line in output.splitlines() if line]


def staged_content(root: Path, path: str) -> bytes:
    """Read raw content for *path* from the Git index."""
    completed = subprocess.run(["git", "show", f":{path}"], cwd=root, capture_output=True)
    if completed.returncode:
        detail = completed.stderr.decode(errors="replace").strip()
        raise GitError(f"could not read staged file {path}: {detail}")
    return completed.stdout


def decode_content(raw: bytes) -> str:
    """Decode staged source as UTF-8, with a latin-1 fallback."""
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        return raw.decode("latin-1")
