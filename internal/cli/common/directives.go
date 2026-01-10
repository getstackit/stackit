package common

import "os"

// HasShellIntegration checks if stackit shell integration is installed.
// The shell wrapper sets STACKIT_SHELL_INTEGRATION=1 when running commands.
func HasShellIntegration() bool {
	return os.Getenv("STACKIT_SHELL_INTEGRATION") == "1"
}
