// Package data exposes the small, embedded datasets used by PhantomGuard.
package data

import _ "embed"

//go:embed aliases.json
var AliasesJSON []byte

//go:embed node_builtins.json
var NodeBuiltinsJSON []byte

//go:embed top_pypi.txt
var TopPyPI []byte

//go:embed top_npm.txt
var TopNPM []byte
