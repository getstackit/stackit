package navigation

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewTrunkCmd creates the trunk command
func NewTrunkCmd() *cobra.Command {
	var (
		add string
		all bool
	)

	cmd := &cobra.Command{
		Use:   "trunk",
		Short: "Show the trunk of the current branch",
		Long: `Show the trunk of the current branch.

By default, displays the trunk branch that the current branch's stack is based on.
Use --all to see all configured trunk branches, or --add to add an additional trunk.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := common.GetGlobalOptions(cmd)
			if add == "" && !all {
				opts = common.ApplyReadOnlyCurrentBranch(opts)
			}

			return common.RunWithOptions(cmd, opts, func(ctx *app.Context) error {
				// Handle --add flag
				if add != "" {
					return handleAddTrunk(ctx, add)
				}

				// Handle --all flag
				if all {
					return handleShowAllTrunks(ctx)
				}

				// Default: show trunk for current branch
				return handleShowTrunk(ctx)
			})
		},
	}

	cmd.Flags().StringVar(&add, "add", "", "Add an additional trunk branch")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all configured trunks")

	return cmd
}

// handleAddTrunk adds a new trunk branch
func handleAddTrunk(ctx *app.Context, trunkName string) error {
	runner := ctx.Git()
	repoRoot := ctx.RepoRoot
	// Verify the branch exists
	branches, err := runner.GetAllBranchNames()
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	found := slices.Contains(branches, trunkName)
	if !found {
		return fmt.Errorf("branch '%s' does not exist", trunkName)
	}

	// Add the trunk
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.AddTrunk(trunkName); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	ctx.Output.Info("Added %s as a trunk branch.", style.ColorBranchName(trunkName, false))
	return nil
}

// handleShowAllTrunks shows all configured trunk branches
func handleShowAllTrunks(ctx *app.Context) error {
	repoRoot := ctx.RepoRoot
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	trunks := cfg.AllTrunks()

	// Get primary trunk to mark it
	primaryTrunk := cfg.Trunk()

	for _, trunk := range trunks {
		if trunk == primaryTrunk {
			ctx.Output.Info("%s (primary)", trunk)
		} else {
			ctx.Output.Info("%s", trunk)
		}
	}

	return nil
}

// handleShowTrunk shows the trunk for the current branch
func handleShowTrunk(ctx *app.Context) error {
	eng := ctx.Engine

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		// Not on a branch, just show primary trunk
		trunk := eng.Trunk()
		ctx.Output.Info("%s", trunk.GetName())
		return nil
	}

	// If current branch is trunk, show it
	if currentBranch.IsTrunk() {
		ctx.Output.Info("%s", currentBranch.GetName())
		return nil
	}

	// Find the trunk by walking up the parent chain
	trunk := findTrunkForBranch(eng, currentBranch.GetName(), ctx.RepoRoot)
	ctx.Output.Info("%s", trunk)
	return nil
}

// findTrunkForBranch walks up the parent chain to find the trunk
func findTrunkForBranch(eng engine.Engine, branchName string, repoRoot string) string {
	// Get all configured trunks
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return eng.Trunk().GetName()
	}
	trunks := cfg.AllTrunks()

	// Walk up the parent chain
	currentBranch := eng.GetBranch(branchName)
	visited := make(map[string]bool)

	for currentBranch.GetName() != "" && !visited[currentBranch.GetName()] {
		visited[currentBranch.GetName()] = true

		// Check if current is a trunk
		if slices.Contains(trunks, currentBranch.GetName()) {
			return currentBranch.GetName()
		}

		// Get parent
		parent := currentBranch.GetParent()
		if parent == nil {
			break
		}
		currentBranch = *parent
	}

	// Default to primary trunk
	return eng.Trunk().GetName()
}
