// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

const prepushHookTemplate = `#!/bin/bash
# Installed by Stackit. To bypass, use --no-verify.
stackit prepush verify
`

// PrepushInstallAction installs the pre-push hook
func PrepushInstallAction(ctx *app.Context) error {
	return installPrepushHook(ctx.RepoRoot, ctx.Output)
}

// PrepushInstallActionWithOutput installs the pre-push hook with a custom writer.
// This is a convenience function for use during init where we don't have an app.Context.
func PrepushInstallActionWithOutput(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)
	return installPrepushHook(repoRoot, out)
}

// installPrepushHook is the core implementation for installing the pre-push hook.
func installPrepushHook(repoRoot string, out output.Output) error {
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "pre-push")

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0750); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// Check if hook already exists
	if _, err := os.Stat(hookPath); err == nil {
		content, err := os.ReadFile(hookPath)
		if err == nil && strings.Contains(string(content), "stackit prepush verify") {
			out.Info("Pre-push hook is already installed.")
			return nil
		}

		// If it exists but doesn't have our command, append
		f, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open existing pre-push hook: %w", err)
		}
		defer func() { _ = f.Close() }()

		if _, err := f.WriteString("\n# Added by Stackit\nstackit prepush verify\n"); err != nil {
			return fmt.Errorf("failed to append to pre-push hook: %w", err)
		}
		out.Info("Appended Stackit verification to existing pre-push hook.")
	} else {
		// Create new hook
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(prepushHookTemplate), 0750); err != nil {
			return fmt.Errorf("failed to write pre-push hook: %w", err)
		}
		out.Info("Installed Stackit pre-push hook.")
	}

	return nil
}

// PrepushVerifyAction verifies that branches being pushed are not locked.
// It reads the refs being pushed from stdin (git pre-push hook protocol).
func PrepushVerifyAction(ctx *app.Context) error {
	return PrepushVerifyFromReader(ctx, os.Stdin)
}

// PrepushVerifyFromReader verifies branches from a reader (for testing).
func PrepushVerifyFromReader(ctx *app.Context, reader io.Reader) error {
	eng := ctx.Engine

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Git pre-push hook provides: <local ref> <local sha> <remote ref> <remote sha>
		// Example: refs/heads/my-branch abc123 refs/heads/my-branch def456
		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}

		localRef := parts[0]

		// Extract branch name from refs/heads/branch-name
		if !strings.HasPrefix(localRef, "refs/heads/") {
			continue // Not a branch ref, skip (could be tags, etc.)
		}

		branchName := strings.TrimPrefix(localRef, "refs/heads/")

		// Check if this branch is managed by stackit
		branch := eng.GetBranch(branchName)
		if !branch.IsTracked() {
			continue // Not a stackit branch, allow push
		}

		// Check if the branch can be modified (not locked or frozen)
		if err := branch.EnsureCanModify(); err != nil {
			return fmt.Errorf("cannot push branch %q: %w", branchName, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read push refs: %w", err)
	}

	return nil
}

// PrepushUninstallAction uninstalls the pre-push hook
func PrepushUninstallAction(ctx *app.Context) error {
	repoRoot := ctx.RepoRoot
	hooksDir := filepath.Join(repoRoot, ".git", "hooks")
	hookPath := filepath.Join(hooksDir, "pre-push")

	// Check if hook exists
	if _, err := os.Stat(hookPath); os.IsNotExist(err) {
		ctx.Output.Info("Pre-push hook is not installed.")
		return nil
	}

	content, err := os.ReadFile(hookPath)
	if err != nil {
		return fmt.Errorf("failed to read pre-push hook: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	newLines := make([]string, 0, len(lines))
	removed := false

	for i := range lines {
		line := lines[i]
		if strings.Contains(line, "stackit prepush verify") {
			removed = true
			// Check if previous line was our comment
			if i > 0 && len(newLines) > 0 && (strings.Contains(lines[i-1], "Installed by Stackit") || strings.Contains(lines[i-1], "Added by Stackit")) {
				newLines = newLines[:len(newLines)-1]
			}
			continue
		}
		newLines = append(newLines, line)
	}

	if !removed {
		ctx.Output.Info("Stackit verification not found in pre-push hook.")
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
			return fmt.Errorf("failed to remove pre-push hook: %w", err)
		}
		ctx.Output.Info("Removed Stackit pre-push hook.")
	} else {
		// Write back modified content
		newContent := strings.Join(newLines, "\n")
		// Clean up trailing/leading newlines that might have been left behind
		newContent = strings.TrimSpace(newContent) + "\n"
		// #nosec G306 - Git hooks need to be executable
		if err := os.WriteFile(hookPath, []byte(newContent), 0750); err != nil {
			return fmt.Errorf("failed to write pre-push hook: %w", err)
		}
		ctx.Output.Info("Removed Stackit verification from existing pre-push hook.")
	}

	return nil
}
