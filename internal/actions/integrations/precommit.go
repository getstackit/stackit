// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"io"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

const (
	precommitHookName    = "pre-commit"
	precommitMarker      = "stackit precommit verify"
	precommitDisplayName = "Pre-commit"
)

const precommitHookTemplate = `#!/bin/bash
# Installed by Stackit. To bypass, use --no-verify.
stackit precommit verify
`

// PrecommitInstallAction installs the pre-commit hook
func PrecommitInstallAction(ctx *app.Context) error {
	return installHook(ctx.RepoRoot, precommitHookName, precommitMarker, precommitHookTemplate, precommitDisplayName, ctx.Output)
}

// PrecommitInstallActionWithOutput installs the pre-commit hook with a custom writer.
// This is a convenience function for use during init where we don't have an app.Context.
func PrecommitInstallActionWithOutput(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)
	return installHook(repoRoot, precommitHookName, precommitMarker, precommitHookTemplate, precommitDisplayName, out)
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
	return uninstallHook(ctx.Output, ctx.RepoRoot, precommitHookName, precommitMarker, precommitDisplayName)
}
