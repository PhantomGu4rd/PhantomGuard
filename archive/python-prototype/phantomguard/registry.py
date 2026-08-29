"""Bounded, concurrent public-registry lookups."""

from __future__ import annotations

import re
import time
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor, as_completed

_PYPI_NAME = re.compile(r"^[a-z0-9][a-z0-9._-]*$", re.IGNORECASE)
_NPM_NAME = re.compile(r"^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$", re.IGNORECASE)
Transport = Callable[[str, dict[str, str], float], int]


def valid_name(ecosystem: str, name: str) -> bool:
    """Validate a name before it can be used in a public registry URL."""
    return bool((_PYPI_NAME if ecosystem == "pypi" else _NPM_NAME).fullmatch(name))


def _url(ecosystem: str, name: str) -> tuple[str, dict[str, str]]:
    if ecosystem == "pypi":
        return f"https://pypi.org/pypi/{name}/json", {}
    encoded = urllib.parse.quote(name, safe="@")
    return f"https://registry.npmjs.org/{encoded}", {
        "Accept": "application/vnd.npm.install-v1+json"
    }


def http_transport(url: str, headers: dict[str, str], timeout: float) -> int:
    """Perform a GET and return its HTTP status without parsing response content."""
    request = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:  # noqa: S310
            return int(response.status)
    except urllib.error.HTTPError as error:
        return int(error.code)


class RegistryClient:
    """Registry validator with injectable transport for fully offline tests."""

    def __init__(self, transport: Transport = http_transport, timeout: float = 3.0) -> None:
        self.transport = transport
        self.timeout = timeout

    def lookup(self, ecosystem: str, name: str) -> str:
        """Return exists, phantom, suspicious, or unknown for one validated candidate."""
        if not valid_name(ecosystem, name):
            return "suspicious"
        try:
            url, headers = _url(ecosystem, name)
            status = self.transport(url, headers, self.timeout)
        except Exception:  # network errors are deliberately not existence claims
            return "unknown"
        if status == 200:
            return "exists"
        if status == 404:
            return "phantom"
        return "unknown"

    def lookup_many(
        self, candidates: set[tuple[str, str]], budget: float = 8.0
    ) -> dict[tuple[str, str], str]:
        """Resolve candidates concurrently within a global time budget."""
        results: dict[tuple[str, str], str] = {}
        started = time.monotonic()
        with ThreadPoolExecutor(max_workers=8) as executor:
            futures = {
                executor.submit(self.lookup, eco, name): (eco, name) for eco, name in candidates
            }
            try:
                for future in as_completed(futures, timeout=budget):
                    results[futures[future]] = future.result()
            except TimeoutError:
                pass
            for future, candidate in futures.items():
                if candidate not in results:
                    future.cancel()
                    results[candidate] = "unknown"
        # A custom transport cannot turn an over-budget scan into "exists".
        if time.monotonic() - started > budget:
            for candidate, status in list(results.items()):
                if status not in {"exists", "phantom", "suspicious"}:
                    results[candidate] = "unknown"
        return results
