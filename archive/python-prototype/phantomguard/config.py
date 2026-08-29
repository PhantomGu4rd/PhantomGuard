"""Configuration loading and precedence rules."""

from __future__ import annotations

import os
import tomllib
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class Config:
    """Effective scan configuration."""

    fail_mode: str = "warn"
    languages: tuple[str, ...] = ("python", "javascript")
    ignore: tuple[str, ...] = ()
    allow: frozenset[str] = frozenset()
    deny: frozenset[str] = frozenset()
    aliases: dict[str, str] = field(default_factory=dict)
    positive_ttl_hours: int = 168
    negative_ttl_hours: int = 1


def load_config(root: Path, strict_flag: bool = False) -> Config:
    """Load `.phantomguard.toml`, then apply environment and CLI precedence."""
    data: dict[str, object] = {}
    config_file = root / ".phantomguard.toml"
    if config_file.is_file():
        with config_file.open("rb") as handle:
            data = tomllib.load(handle).get("phantomguard", {})
    if not isinstance(data, dict):
        data = {}
    cache = data.get("cache", {})
    if not isinstance(cache, dict):
        cache = {}
    aliases = data.get("aliases", {})
    if not isinstance(aliases, dict):
        aliases = {}
    mode = str(data.get("fail_mode", "warn")).lower()
    env_mode = os.environ.get("PHANTOMGUARD_FAIL_MODE")
    if env_mode:
        mode = env_mode.lower()
    if strict_flag or os.environ.get("PHANTOMGUARD_STRICT") == "1":
        mode = "strict"
    if mode not in {"warn", "strict"}:
        mode = "warn"
    return Config(
        fail_mode=mode,
        languages=tuple(str(x) for x in data.get("languages", ("python", "javascript"))),
        ignore=tuple(str(x) for x in data.get("ignore", ())),
        allow=frozenset(str(x) for x in data.get("allow", ())),
        deny=frozenset(str(x) for x in data.get("deny", ())),
        aliases={str(key): str(value) for key, value in aliases.items()},
        positive_ttl_hours=int(cache.get("positive_ttl_hours", 168)),
        negative_ttl_hours=int(cache.get("negative_ttl_hours", 1)),
    )
