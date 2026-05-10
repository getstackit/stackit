// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

const precommitHookTemplate = `#!/bin/bash
# Installed by Stackit. To bypass, use --no-verify.
stackit precommit verify
`

// PrecommitInstallAction installs the pre-commit hook
func PrecommitInstallAction(ctx *app.Context) error {
	return installPrecommitHook(ctx.RepoRoot, ctx.Output)
}

// PrecommitInstallActionWithOutput installs the pre-commit hook with a custom writer.
// This is a convenience function for use during init where we don't have an app.Context.
func PrecommitInstallActionWithOutput(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)
	return installPrecommitHook(repoRoot, out)
}

// installPrecommitHook is the core implementation for installing the pre-commit hook.
func installPrecommitHook(repoRoot string, out output.Output) error {
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "pre-commit")

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0750); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// Check if hook already exists
	if _, err := os.Stat(hookPath); err == nil {
		content, err := os.ReadFile(hookPath)
		if err == nil && strings.Contains(string(content), "stackit precommit verify") {
			out.Info("Pre-commit hook is already installed.")
			return nil
		}

		// If it exists but doesn't have our command, append
		f, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open existing pre-commit hook: %w", err)
		}
		defer func() { _ = f.Close() }()

		if _, err := f.WriteString("\n# Added by Stackit\nstackit precommit verify\n"); err != nil {
			return fmt.Errorf("failed to append to pre-commit hook: %w", err)
		}
		out.Info("Appended Stackit verification to existing pre-commit hook.")
	} else {
		// Create new hook
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(precommitHookTemplate), 0750); err != nil {
			return fmt.Errorf("failed to write pre-commit hook: %w", err)
		}
		out.Info("Installed Stackit pre-commit hook.")
	}

	return nil
}

// PrecommitVerifyAction checks if the current branch can be modified
func PrecommitVerifyAction(ctx *app.Context) error {
	eng := ctx.Engine
	currentBranch := eng.CurrentBranch()

	if currentBranch == nil {
		// Not on a branch, allow the commit (e.g. initial commit or detached HEAD)
		return nil
	}

	// EnsureCanModify will return a BranchModificationError if locked or frozen
	return currentBranch.EnsureCanModify()
}

// PrecommitUninstallAction uninstalls the pre-commit hook
func PrecommitUninstallAction(ctx *app.Context) error {
	repoRoot := ctx.RepoRoot
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "pre-commit")

	// Check if hook exists
	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		ctx.Output.Info("Pre-commit hook is not installed.")
		return nil
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		return fmt.Errorf("failed to read pre-commit hook: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	newLines := make([]string, 0, len(lines))
	removed := false

	for i := range lines {
		line := lines[i]
		if strings.Contains(line, "stackit precommit verify") {
			removed = true
			// Check if previous line was our comment or if it's the "# Added by Stackit" comment
			if i > 0 && len(newLines) > 0 && (strings.Contains(lines[i-1], "Installed by Stackit") || strings.Contains(lines[i-1], "Added by Stackit")) {
				newLines = newLines[:len(newLines)-1]
			}
			continue
		}
		newLines = append(newLines, line)
	}

	if !removed {
		ctx.Output.Info("Stackit verification not found in pre-commit hook.")
		return nil
	}

	// Check if only shebang is left or if it's empty
	isOnlyShebang := true
	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#!") {
			continue
		}
		isOnlyShebang = false
		break
	}

	if isOnlyShebang {
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("failed to remove pre-commit hook: %w", err)
		}
		ctx.Output.Info("Removed Stackit pre-commit hook.")
	} else {
		// Write back modified content
		newContent := strings.Join(newLines, "\n")
		// Clean up trailing/leading newlines that might have been left behind
		newContent = strings.TrimSpace(newContent) + "\n"
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(newContent), 0750); err != nil {
			return fmt.Errorf("failed to write pre-commit hook: %w", err)
		}
		ctx.Output.Info("Removed Stackit verification from existing pre-commit hook.")
	}

	return nil
}
