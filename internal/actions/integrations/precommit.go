// Package integrations provides functionality for integrating Stackit with external tools and hooks.
package integrations

import (
	"context"
	"io"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Engine lists exactly the engine methods the hook integrations call.
type Engine interface {
	engine.BranchLookup
	CurrentBranch(ctx context.Context) *engine.Branch
}

// Deps are the collaborators the hook integrations need. Adapters build them
// from their own runtime state; the actions never see bootstrap types.
type Deps struct {
	Engine   Engine
	Out      output.Output
	RepoRoot string
}

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
func PrecommitInstallAction(deps Deps) error {
	return installHook(deps.RepoRoot, precommitHookName, precommitMarker, precommitHookTemplate, precommitDisplayName, deps.Out)
}

// PrecommitInstallActionWithOutput installs the pre-commit hook with a custom writer.
// This is a convenience function for use during init where we don't have full Deps.
func PrecommitInstallActionWithOutput(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)
	return installHook(repoRoot, precommitHookName, precommitMarker, precommitHookTemplate, precommitDisplayName, out)
}

// PrecommitVerifyAction checks if the current branch can be modified
func PrecommitVerifyAction(ctx context.Context, deps Deps) error {
	currentBranch := deps.Engine.CurrentBranch(ctx)

	if currentBranch == nil {
		// Not on a branch, allow the commit (e.g. initial commit or detached HEAD)
		return nil
	}

	// EnsureCanModify will return a BranchModificationError if locked or frozen
	return currentBranch.EnsureCanModify()
}

// PrecommitUninstallAction uninstalls the pre-commit hook
func PrecommitUninstallAction(deps Deps) error {
	return uninstallHook(deps.Out, deps.RepoRoot, precommitHookName, precommitMarker, precommitDisplayName)
}
