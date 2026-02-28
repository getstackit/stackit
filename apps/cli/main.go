// Package main is the entry point for the stackit CLI tool.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"stackit.dev/stackit/internal/cli"
	"stackit.dev/stackit/internal/output"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	exitCode := run()
	os.Exit(exitCode)
}

func run() int {
	// Set up crash logger for panic recovery
	logger := output.NewFileLoggerOrNull(output.GetLogFilePath())
	defer func() { _ = logger.Close() }()

	defer func() {
		if p := recover(); p != nil {
			stack := string(debug.Stack())
			// Log to dedicated panic file for easy debugging
			output.LogPanic(p, stack)
			// Also log to regular log file
			logger.Error("stackit crashed: %v\n%s", p, stack)
			fmt.Fprintf(os.Stderr, "stackit crashed: %v\nDetails logged to: %s\n", p, output.GetPanicLogPath())
		}
	}()

	// Check for passthrough commands before processing with cobra
	if cli.HandlePassthrough(os.Args) {
		return 0 // HandlePassthrough already exited
	}

	rootCmd := cli.NewRootCmd(version, commit, date)
	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}
