// Package main is the entry point for the stackit CLI tool.
package main

import (
	"os"
	"runtime/debug"

	"stackit.dev/stackit/internal/cli"
	"stackit.dev/stackit/internal/splog"
	"stackit.dev/stackit/internal/utils"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
	// Capture all panics and log them to the file log
	defer func() {
		if r := recover(); r != nil {
			splog.LogPanic(r, debug.Stack())
			// Ensure terminal is restored if we were in a TUI
			utils.RestoreTerminal()
			// Re-panic to ensure the application still exits with the original panic
			panic(r)
		}
	}()

	if err := run(); err != nil {
		return 1
	}
	return 0
}

func run() error {
	// Check for passthrough commands before processing with cobra
	if cli.HandlePassthrough(os.Args) {
		return nil // HandlePassthrough already exited
	}

	rootCmd := cli.NewRootCmd(version, commit, date)
	return rootCmd.Execute()
}
