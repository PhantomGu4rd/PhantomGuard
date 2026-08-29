"""Git pre-commit hook installation with safe foreign-hook handling."""

from __future__ import annotations

from pathlib import Path

HOOK = """#!/bin/sh
# Installed by PhantomGuard.
phantomguard scan --staged || exit $?
"""


def install(root: Path, force: bool = False) -> str:
    """Install the hook, refusing to overwrite a non-PhantomGuard hook by default."""
    hook = root / ".git" / "hooks" / "pre-commit"
    if not (root / ".git").is_dir():
        raise RuntimeError("not inside a Git repository")
    if hook.exists() and "Installed by PhantomGuard" not in hook.read_text(
        encoding="utf-8", errors="replace"
    ):
        if not force:
            raise RuntimeError("a foreign pre-commit hook exists; rerun with --force to back it up")
        backup = hook.with_name("pre-commit.phantomguard-backup")
        hook.replace(backup)
        hook.write_text(HOOK + f'"{backup.as_posix()}"\n', encoding="utf-8")
    else:
        hook.write_text(HOOK, encoding="utf-8")
    try:
        hook.chmod(hook.stat().st_mode | 0o111)
    except OSError:
        pass
    return str(hook)
