from phantomguard.extract.javascript_src import extract as js_extract
from phantomguard.extract.manifests import package_json, pyproject, requirements
from phantomguard.extract.python_src import extract as python_extract


def names(findings):
    return {(item.name, item.line, item.confidence) for item in findings}


def test_python_ast_extracts_and_skips_relative_and_dynamic() -> None:
    content = """import requests.sessions
from yaml.loader import SafeLoader
from .local import thing
importlib.import_module('dateutil.parser')
__import__(variable)
"""
    found, unscannable, fallback = python_extract("x.py", content)
    assert names(found) == {("requests", 1, "high"), ("yaml", 2, "high"), ("dateutil", 4, "high")}
    assert unscannable == 1
    assert not fallback


def test_python_syntax_fallback() -> None:
    found, _, fallback = python_extract("x.py", "import ghost_pkg\nif :\n")
    assert names(found) == {("ghost_pkg", 1, "reduced")}
    assert fallback


def test_javascript_extracts_literals_and_filters_non_packages() -> None:
    content = """// import nope from 'commented'
import type { A } from '@scope/pkg/sub';
export { x } from 'lodash/get';
import 'normalize.css';
const fs = require('node:fs');
const x = import('axios');
import '#alias';
"""
    found, _, _ = js_extract("web.ts", content)
    assert {item.name for item in found} == {"@scope/pkg", "lodash", "normalize.css", "axios"}


def test_manifests_normalize_and_skip_local_specs() -> None:
    found, _ = requirements(
        "requirements.txt", "requests[security]>=2; python_version > '3'\n-e .\ngit+https://x\n"
    )
    assert [item.name for item in found] == ["requests"]
    found, _ = pyproject(
        "pyproject.toml",
        "[project]\n"
        "dependencies = ['flask>=3']\n"
        "[project.optional-dependencies]\n"
        "test = ['pytest']\n",
    )
    assert {item.name for item in found} == {"flask", "pytest"}
    found, _ = package_json(
        "package.json", '{"dependencies":{"react":"^18", "local":"workspace:*", "url":"https://x"}}'
    )
    assert [item.name for item in found] == ["react"]
