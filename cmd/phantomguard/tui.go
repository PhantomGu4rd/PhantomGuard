package main

import (
	"os"

	"github.com/phantomguard/phantomguard/pkg/buildinfo"
	"github.com/phantomguard/phantomguard/pkg/tui"
)

// tuiCommand keeps the interactive terminal interface separate from the hook
// command implementation while sharing the same production scanner packages.
func tuiCommand(root string, arguments []string) int {
	return tui.Run(tui.Options{
		Root:    root,
		Args:    arguments,
		In:      os.Stdin,
		Out:     os.Stdout,
		Err:     os.Stderr,
		Version: buildinfo.Version,
	})
}
