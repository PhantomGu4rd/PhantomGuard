from pathlib import Path

from phantomguard.cache import Cache
from phantomguard.policy import exit_code
from phantomguard.registry import RegistryClient, valid_name


def test_registry_is_injectable_and_validates_before_urls() -> None:
    calls: list[str] = []

    def transport(url, headers, timeout):
        calls.append(url)
        return 404 if "ghost" in url else 200

    client = RegistryClient(transport=transport)
    assert client.lookup("pypi", "requests") == "exists"
    assert client.lookup("npm", "@scope/ghost") == "phantom"
    assert client.lookup("pypi", "bad/name") == "suspicious"
    assert len(calls) == 2
    assert not valid_name("npm", "@scope")


def test_cache_ttls_and_unknown_not_cached(tmp_path: Path) -> None:
    clock = [1000.0]
    cache = Cache(tmp_path / "cache.json", now=lambda: clock[0])
    cache.put("pypi", "requests", "exists")
    assert cache.get("pypi", "requests", 1, 1) == "exists"
    clock[0] += 3601
    assert cache.get("pypi", "requests", 1, 1) is None
    cache.put("pypi", "nope", "unknown")
    assert cache.get("pypi", "nope", 1, 1) is None


def test_policy_matrix() -> None:
    assert exit_code(["exists", "unknown"], "warn") == 0
    assert exit_code(["phantom"], "warn") == 1
    assert exit_code(["unknown"], "strict") == 1
    assert exit_code(["suspicious"], "warn") == 1
