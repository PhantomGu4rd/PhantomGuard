package extractor

import "testing"

func TestPythonExtractionSkipsRelativeAndFindsLiteralDynamicImport(t *testing.T) {
	findings, unscannable := Python("app.py", "import requests as r, localmod\nfrom . import tools\nfrom pandas.core import frame\nimportlib.import_module('cv2')\n__import__(dynamic)")
	if unscannable != 1 || len(findings) != 4 {
		t.Fatalf("got %d findings and %d unscannable imports", len(findings), unscannable)
	}
	if findings[0].Name != "requests" || findings[2].Name != "pandas" || findings[3].Name != "cv2" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestPythonExtractionIgnoresCommentsAndTripleQuotedTestFixtures(t *testing.T) {
	source := "'''\nimport phantom_fixture\n'''\n# import ignored_comment\nimport requests\n"
	findings, _ := Python("test_app.py", source)
	if len(findings) != 1 || findings[0].Name != "requests" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestJavaScriptExtractionIgnoresCommentsRelativePathsAndBuiltins(t *testing.T) {
	source := "// import nope from 'ghost'\nimport react from 'react';\nconst fs = require('node:fs');\nconst local = require('./local');\nexport {x} from '@scope/pkg/sub';"
	findings, _ := JavaScript("app.ts", source)
	if len(findings) != 2 || findings[0].Name != "react" || findings[1].Name != "@scope/pkg" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestGoExtractionUsesASTAndSkipsStandardAndLocalPackages(t *testing.T) {
	findings, err := GoSource("main.go", "package main\nimport (\n \"fmt\"\n \"github.com/acme/lib/v2\"\n \"github.com/example/project/local\"\n)", "github.com/example/project/")
	if err != nil || len(findings) != 1 || findings[0].Name != "github.com/acme/lib/v2" {
		t.Fatalf("findings=%#v err=%v", findings, err)
	}
}

func TestGoExtractionKeepsThirdPartyImportsWhenNoModuleFileExists(t *testing.T) {
	findings, err := GoSource("main.go", "package main\nimport \"github.com/acme/lib\"", "")
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%#v err=%v", findings, err)
	}
}
