// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

const (
	prepushHookName    = "pre-push"
	prepushMarker      = "stackit prepush verify"
	prepushDisplayName = "Pre-push"
)

const prepushHookTemplate = `#!/bin/bash
# Installed by Stackit. To bypass, use --no-verify.
stackit prepush verify
`

// PrepushInstallAction installs the pre-push hook
func PrepushInstallAction(ctx *app.Context) error {
	return installHook(ctx.RepoRoot, prepushHookName, prepushMarker, prepushHookTemplate, prepushDisplayName, ctx.Output)
}

// PrepushInstallActionWithOutput installs the pre-push hook with a custom writer.
// This is a convenience function for use during init where we don't have an app.Context.
func PrepushInstallActionWithOutput(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)
	return installHook(repoRoot, prepushHookName, prepushMarker, prepushHookTemplate, prepushDisplayName, out)
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
	return uninstallHook(ctx.Output, ctx.RepoRoot, prepushHookName, prepushMarker, prepushDisplayName)
}
