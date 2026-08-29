package extractor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/phantomguard/phantomguard/pkg/model"
)

// GoSource uses Go's parser and AST packages; it never compiles or evaluates scanned source.
func GoSource(path, content, modulePrefix string) ([]model.Finding, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, content, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var findings []model.Finding
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath == "" || isGoStandardLibrary(importPath) || modulePrefix != "" && strings.HasPrefix(importPath, modulePrefix) {
			continue
		}
		position := fileSet.Position(spec.Pos())
		findings = append(findings, model.Finding{
			Name: importPath, Ecosystem: model.Go, Path: path, Line: position.Line, Snippet: contextAt(content, position.Line),
		})
	}
	return findings, nil
}

// isGoStandardLibrary follows Go's import convention: standard imports have no domain-like first path segment.
func isGoStandardLibrary(importPath string) bool {
	first := strings.Split(importPath, "/")[0]
	return !strings.Contains(first, ".")
}

// Keep go/ast in the import set: ImportSpec documents the AST type we traverse above.
var _ *ast.ImportSpec
