// Package inprocess provides in-process CLI execution for tests.
package inprocess

import (
	"bytes"
	"strings"

	"stackit.dev/stackit/internal/cli"
)

// CLI provides in-process CLI execution for faster tests.
// Instead of spawning a new process for each command, it calls the CLI directly.
type CLI struct{}

// Result contains the result of an in-process CLI execution.
type Result struct {
	Output string
	Err    error
}

// NewInProcessCLI creates a new in-process CLI runner.
func NewInProcessCLI() *CLI {
	return &CLI{}
}

// Run executes a stackit command in-process.
// The workDir specifies the working directory for the command.
// Returns the combined stdout/stderr output and any error.
func (c *CLI) Run(workDir string, args ...string) Result {
	// Build args with --cwd for working directory and --no-interactive
	fullArgs := []string{"stackit", "--cwd", workDir, "--no-interactive"}
	fullArgs = append(fullArgs, args...)

	// Capture output
	var buf bytes.Buffer

	// Check for passthrough commands
	if handled, err := cli.HandlePassthroughWithResult(fullArgs, false, &buf, &buf); handled {
		output := buf.String()
		if err != nil && output == "" {
			output = err.Error()
		}
		return Result{
			Output: output,
			Err:    err,
		}
	}

	// Create a new root command for each execution
	// The new runner architecture doesn't use global state, so no reset needed
	rootCmd := cli.NewRootCmd("test", "test", "test")

	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Set args (excluding the program name "stackit")
	rootCmd.SetArgs(fullArgs[1:])

	// Execute
	err := rootCmd.Execute()

	return Result{
		Output: buf.String(),
		Err:    err,
	}
}

// RunString executes a stackit command from a single string (like "create feature -m 'test'").
func (c *CLI) RunString(workDir string, cmdStr string) Result {
	args := splitInProcessArgs(cmdStr)
	return c.Run(workDir, args...)
}

// splitInProcessArgs splits a command string into args, respecting quotes.
func splitInProcessArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			switch {
			case inQuote && r == quoteChar:
				inQuote = false
			case !inQuote:
				inQuote = true
				quoteChar = r
			default:
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
