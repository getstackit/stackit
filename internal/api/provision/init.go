package provision

import (
	"context"
	"fmt"
	"slices"

	initaction "github.com/getstackit/stackit/internal/actions/init"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/git"
)

// EnsureInitialized makes repoRoot a stackit repo if it is not one already, so a
// freshly cloned checkout can be served (app.GetContext refuses uninitialized
// repos). trunk is the preferred trunk branch — typically the GitHub default
// branch — and is inferred when empty or absent locally. Initializing an
// already-initialized repo is a no-op, so this is safe on the boot path too.
func EnsureInitialized(ctx context.Context, repoRoot, trunk string) error {
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.IsInitialized() {
		return nil
	}

	runner := git.NewRunnerWithPath(repoRoot, nil)
	branchNames, err := runner.GetAllBranchNames(ctx)
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	if len(branchNames) == 0 {
		return fmt.Errorf("repository has no branches")
	}

	if trunk == "" || !slices.Contains(branchNames, trunk) {
		trunk = initaction.InferTrunk(ctx, runner, branchNames)
	}
	if trunk == "" {
		return fmt.Errorf("could not determine trunk branch")
	}

	if err := cfg.SetTrunk(trunk); err != nil {
		return fmt.Errorf("set trunk: %w", err)
	}
	return nil
}
