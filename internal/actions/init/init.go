// Package init provides functionality for initializing Stackit in a Git repository.
package init

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// Options contains options for the init action
type Options struct {
	Trunk string
	Reset bool
}

// Handler abstracts interaction for the init action
type Handler interface {
	// SelectTrunk prompts the user to select the trunk branch
	SelectTrunk(ctx context.Context, branchNames []string, inferredTrunk string) (string, error)

	// OnSuccess is called when initialization finishes
	OnSuccess(trunkName string, wasInitialized bool, isReset bool)
}

// Action performs the initialization of Stackit in a repository
func Action(ctx context.Context, repoRoot string, opts Options, handler Handler) error {
	runner := git.NewRunnerWithPath(repoRoot, nil)

	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	branchNames, err := runner.GetAllBranchNames(ctx)
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	if len(branchNames) == 0 {
		return fmt.Errorf("no branches found in current repo; cannot initialize Stackit.\nPlease create your first commit and then re-run stackit init")
	}

	trunkName := opts.Trunk
	if trunkName == "" {
		inferredTrunk := InferTrunk(ctx, runner, branchNames)
		selected, err := handler.SelectTrunk(ctx, branchNames, inferredTrunk)
		if err != nil {
			return err
		}
		trunkName = selected
	} else {
		found := slices.Contains(branchNames, trunkName)
		if !found {
			return fmt.Errorf("branch '%s' not found", trunkName)
		}
	}

	wasInitialized := cfg.IsInitialized()

	if err := cfg.SetTrunk(trunkName); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	maxUndoDepth := cfg.UndoStackDepth()

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          repoRoot,
		Trunk:             trunkName,
		MaxUndoStackDepth: maxUndoDepth,
	})
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	if opts.Reset {
		if err := eng.Reset(trunkName); err != nil {
			return fmt.Errorf("failed to reset branches: %w", err)
		}
	} else {
		if err := eng.Rebuild(trunkName); err != nil {
			return fmt.Errorf("failed to rebuild engine: %w", err)
		}
	}

	handler.OnSuccess(trunkName, wasInitialized, opts.Reset)

	return nil
}

// InferTrunk attempts to infer the trunk branch name
func InferTrunk(ctx context.Context, runner git.Runner, branchNames []string) string {
	remoteBranch, err := runner.FindRemoteBranch(ctx, "origin")
	if err == nil && remoteBranch != "" {
		if slices.Contains(branchNames, remoteBranch) {
			return remoteBranch
		}
	}

	if common := FindCommonlyNamedTrunk(branchNames); common != "" {
		return common
	}

	// Fallback to current branch
	current, err := runner.GetCurrentBranch()
	if err == nil && current != "" {
		return current
	}

	return ""
}

// FindCommonlyNamedTrunk checks for common trunk branch names
// Returns the branch name if exactly one is found, empty string otherwise
func FindCommonlyNamedTrunk(branchNames []string) string {
	commonNames := []string{"main", "master", "development", "develop"}
	var found []string

	for _, name := range branchNames {
		if slices.Contains(commonNames, name) {
			found = append(found, name)
		}
	}

	if len(found) == 1 {
		return found[0]
	}
	return ""
}
