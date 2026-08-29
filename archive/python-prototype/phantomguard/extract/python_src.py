"""AST-based Python dependency extraction without executing source."""

from __future__ import annotations

import ast
import re

from phantomguard.models import Finding

_FALLBACK = re.compile(r"^\s*(?:import|from)\s+([A-Za-z_]\w*(?:\.\w+)*)")


class _Imports(ast.NodeVisitor):
    def __init__(self, path: str) -> None:
        self.path = path
        self.findings: list[Finding] = []
        self.unscannable = 0

    def _add(self, name: str, line: int) -> None:
        self.findings.append(Finding(name.split(".", 1)[0], "pypi", self.path, line))

    def visit_Import(self, node: ast.Import) -> None:  # noqa: N802
        for alias in node.names:
            self._add(alias.name, node.lineno)

    def visit_ImportFrom(self, node: ast.ImportFrom) -> None:  # noqa: N802
        if node.level == 0 and node.module:
            self._add(node.module, node.lineno)

    def visit_Call(self, node: ast.Call) -> None:  # noqa: N802
        is_import_fn = isinstance(node.func, ast.Name) and node.func.id == "__import__"
        is_importlib = (
            isinstance(node.func, ast.Attribute)
            and node.func.attr == "import_module"
            and isinstance(node.func.value, ast.Name)
            and node.func.value.id == "importlib"
        )
        if is_import_fn or is_importlib:
            if (
                node.args
                and isinstance(node.args[0], ast.Constant)
                and isinstance(node.args[0].value, str)
            ):
                self._add(node.args[0].value, node.lineno)
            else:
                self.unscannable += 1
        self.generic_visit(node)


def extract(path: str, content: str) -> tuple[list[Finding], int, bool]:
    """Extract Python imports, returning findings, dynamic count, fallback-used."""
    try:
        tree = ast.parse(content, filename=path)
    except SyntaxError:
        findings = [
            Finding(match.group(1).split(".", 1)[0], "pypi", path, index, "reduced")
            for index, line in enumerate(content.splitlines(), 1)
            if (match := _FALLBACK.match(line))
        ]
        return findings, 0, True
    visitor = _Imports(path)
    visitor.visit(tree)
    return visitor.findings, visitor.unscannable, False
