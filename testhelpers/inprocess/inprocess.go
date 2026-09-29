// Package inprocess provides in-process CLI execution for tests.
package inprocess

import (
	"bytes"

	"github.com/getstackit/stackit/internal/cli"
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
	fullArgs := make([]string, 0, 4+len(args))
	fullArgs = append(fullArgs, "stackit", "--cwd", workDir, "--no-interactive")
	fullArgs = append(fullArgs, args...)

	// Capture output
	var buf bytes.Buffer

	// Check for passthrough commands
	if handled, err := cli.HandlePassthroughWithResult(fullArgs, cli.PassthroughReturnError, &buf, &buf); handled {
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
