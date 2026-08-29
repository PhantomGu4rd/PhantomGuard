"""Atomic local result cache with policy-specific TTLs."""

from __future__ import annotations

import json
import os
import tempfile
import time
from pathlib import Path


class Cache:
    """JSON-backed registry cache; unknown outcomes are intentionally never stored."""

    def __init__(self, path: Path | None = None, now: callable = time.time) -> None:
        cache_home = Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache"))
        self.path = path or cache_home / "phantomguard" / "cache.json"
        self.now = now

    def _read(self) -> dict[str, dict[str, object]]:
        try:
            data = json.loads(self.path.read_text(encoding="utf-8"))
            return data if isinstance(data, dict) else {}
        except (OSError, json.JSONDecodeError):
            return {}

    def get(
        self, ecosystem: str, name: str, positive_hours: int, negative_hours: int
    ) -> str | None:
        """Return a fresh cached status, if any."""
        item = self._read().get(f"{ecosystem}:{name}")
        if not isinstance(item, dict) or item.get("status") not in {"exists", "phantom"}:
            return None
        ttl = positive_hours if item["status"] == "exists" else negative_hours
        if self.now() - float(item.get("checked_at", 0)) > ttl * 3600:
            return None
        return str(item["status"])

    def put(self, ecosystem: str, name: str, status: str) -> None:
        """Persist a definitive registry result atomically."""
        if status not in {"exists", "phantom"}:
            return
        data = self._read()
        data[f"{ecosystem}:{name}"] = {"status": status, "checked_at": self.now()}
        self.path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, tmp = tempfile.mkstemp(dir=self.path.parent, prefix="cache-", suffix=".json")
        try:
            with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
                json.dump(data, handle, sort_keys=True)
            os.replace(tmp, self.path)
        finally:
            if os.path.exists(tmp):
                os.unlink(tmp)

    def clear(self) -> None:
        """Clear all cached entries."""
        if self.path.exists():
            self.path.unlink()
