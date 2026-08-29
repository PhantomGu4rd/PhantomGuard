import subprocess
import sys
from pathlib import Path

from phantomguard.cache import Cache
from phantomguard.config import load_config
from phantomguard.installer import install
from phantomguard.registry import RegistryClient
from phantomguard.report import render
from phantomguard.scanner import scan_contents


def test_config_precedence(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / ".phantomguard.toml").write_text(
        "[phantomguard]\nfail_mode='warn'\n", encoding="utf-8"
    )
    monkeypatch.setenv("PHANTOMGUARD_FAIL_MODE", "strict")
    assert load_config(tmp_path).fail_mode == "strict"
    assert load_config(tmp_path, strict_flag=True).fail_mode == "strict"


def test_end_to_end_mocked_registry_blocks_phantom(tmp_path: Path) -> None:
    def transport(url, headers, timeout):
        return 404 if "flask-session-manager" in url else 200

    config = load_config(tmp_path)
    report = scan_contents(
        tmp_path,
        {"app.py": "import requests\nimport flask_session_manager\n"},
        config,
        RegistryClient(transport),
        Cache(tmp_path / "cache.json"),
    )
    output, code = render(report, "warn")
    assert code == 1
    assert "flask-session-manager" in output
    assert "flask-session" in output


def test_install_refuses_foreign_hook_and_force_chains_it(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q"], cwd=tmp_path, check=True)
    hook = tmp_path / ".git" / "hooks" / "pre-commit"
    hook.write_text("#!/bin/sh\necho foreign\n", encoding="utf-8")
    try:
        install(tmp_path)
    except RuntimeError as error:
        assert "foreign" in str(error)
    else:
        raise AssertionError("foreign hook was overwritten")
    install(tmp_path, force=True)
    installed = hook.read_text(encoding="utf-8")
    assert "phantomguard scan --staged || exit $?" in installed
    assert "pre-commit.phantomguard-backup" in installed


def test_hook_installer_blocks_a_staged_phantom(tmp_path: Path) -> None:
    subprocess.run(["git", "init", "-q"], cwd=tmp_path, check=True)
    subprocess.run(["git", "config", "user.email", "test@example.test"], cwd=tmp_path, check=True)
    subprocess.run(["git", "config", "user.name", "Test"], cwd=tmp_path, check=True)
    # Hook execution uses this interpreter module entrypoint in a test subprocess.
    hook = tmp_path / ".git" / "hooks" / "pre-commit"
    hook.write_text(
        f"#!{sys.executable}\n"
        "import sys\n"
        "from phantomguard.cli import main\n"
        'raise SystemExit(main(["scan", "--staged"]))\n'
    )
    hook.chmod(0o755)
    (tmp_path / ".phantomguard.toml").write_text(
        "[phantomguard]\nfail_mode='strict'\ndeny=['definitely-missing-pkg']\n",
        encoding="utf-8",
    )
    (tmp_path / "app.py").write_text("import definitely_missing_pkg\n", encoding="utf-8")
    subprocess.run(["git", "add", "app.py"], cwd=tmp_path, check=True)
    result = subprocess.run(
        ["git", "commit", "-m", "blocked"],
        cwd=tmp_path,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 1
    assert "definitely-missing-pkg" in result.stdout + result.stderr
