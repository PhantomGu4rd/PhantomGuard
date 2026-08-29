"""Command-line entry point for PhantomGuard."""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

from phantomguard.cache import Cache
from phantomguard.config import load_config
from phantomguard.gitio import GitError, decode_content, repo_root, staged_content, staged_files
from phantomguard.installer import install
from phantomguard.report import render
from phantomguard.resolve import ignored
from phantomguard.scanner import relevant, scan_contents, working_tree_contents


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="phantomguard", description="Block phantom registry dependencies."
    )
    subcommands = parser.add_subparsers(dest="command", required=True)
    scan = subcommands.add_parser(
        "scan", help="scan working-tree files, all files, or staged files"
    )
    scan.add_argument("paths", nargs="*", help="repository-relative paths to scan")
    scan.add_argument("--all", action="store_true", help="scan all supported files")
    scan.add_argument("--staged", action="store_true", help="scan staged Git-index content")
    scan.add_argument("--strict", action="store_true", help="block when registries are unavailable")
    scan.add_argument("--json", action="store_true", help="emit a machine-readable report")
    hook = subcommands.add_parser("install", help="install the Git pre-commit hook")
    hook.add_argument("--force", action="store_true", help="back up and chain a foreign hook")
    cache = subcommands.add_parser("cache", help="manage local registry result cache")
    cache_sub = cache.add_subparsers(dest="cache_command", required=True)
    cache_sub.add_parser("clear", help="clear cached registry results")
    return parser


def _staged(root: Path, config) -> tuple[dict[str, str], list[str]]:
    contents: dict[str, str] = {}
    notices: list[str] = []
    for path in staged_files(root):
        if not relevant(path) or ignored(path, config):
            continue
        try:
            raw = staged_content(root, path)
        except GitError as error:
            notices.append(str(error))
            continue
        if len(raw) > 1_048_576:
            notices.append(f"{path}: skipped (>1 MiB)")
            continue
        contents[path] = decode_content(raw)
    return contents, notices


def _scan(args: argparse.Namespace, root: Path) -> int:
    config = load_config(root, args.strict)
    if os.environ.get("PHANTOMGUARD_SKIP") == "1":
        print("PhantomGuard skipped because PHANTOMGUARD_SKIP=1")
        return 0
    if args.staged:
        contents, notices = _staged(root, config)
    else:
        paths = args.paths if args.paths else None
        if not args.all and not paths:
            raise ValueError("choose --all, --staged, or one or more paths")
        contents, notices = working_tree_contents(root, paths)
        contents = {path: value for path, value in contents.items() if not ignored(path, config)}
    try:
        report = scan_contents(root, contents, config)
        report.notices = notices + report.notices
        output, code = render(report, config.fail_mode, args.json)
        print(output)
        return code
    except Exception as error:
        # A hook failure must be truthful and mode-aware, never a raw traceback.
        message = f"PhantomGuard internal error: {error}. "
        if config.fail_mode == "strict":
            print(message + "Blocking because fail_mode=strict.", file=sys.stderr)
            return 1
        print(message + "Allowing commit loudly because fail_mode=warn.", file=sys.stderr)
        return 0


def main(argv: list[str] | None = None) -> int:
    """Run PhantomGuard and return a documented process exit code."""
    args = _parser().parse_args(argv)
    try:
        root = repo_root(Path.cwd())
    except GitError as error:
        print(f"PhantomGuard: {error}", file=sys.stderr)
        return 2
    try:
        if args.command == "install":
            print(f"Installed PhantomGuard hook at {install(root, args.force)}")
            return 0
        if args.command == "cache" and args.cache_command == "clear":
            Cache().clear()
            print("PhantomGuard cache cleared")
            return 0
        return _scan(args, root)
    except (RuntimeError, ValueError) as error:
        print(f"PhantomGuard: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
