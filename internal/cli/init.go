package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	initaction "stackit.dev/stackit/internal/actions/init"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
)

// cliInitHandler implements initaction.Handler for the CLI
type cliInitHandler struct {
	noInteractive bool
	writer        io.Writer
}

func (h *cliInitHandler) SelectTrunk(_ context.Context, branchNames []string, inferredTrunk string) (string, error) {
	interactive := !h.noInteractive && tui.IsTTY()
	if !interactive {
		if inferredTrunk != "" {
			return inferredTrunk, nil
		}
		return "", fmt.Errorf("could not infer trunk branch, pass in an existing branch name with --trunk or run in interactive mode")
	}

	choices := make([]tui.BranchChoice, len(branchNames))
	initialIndex := 0
	for i, name := range branchNames {
		choices[i] = tui.BranchChoice{
			Display: name,
			Value:   name,
		}
		if name == inferredTrunk {
			initialIndex = i
		}
	}

	selected, err := tui.PromptBranchSelection("Select your trunk branch (main development branch):", choices, initialIndex)
	if err != nil {
		return "", err
	}

	return selected, nil
}

func (h *cliInitHandler) OnSuccess(trunkName string, wasInitialized bool, isReset bool) {
	splog := tui.NewSplogToWriter(h.writer)

	if wasInitialized {
		splog.Info("Reinitializing Stackit...")
	} else {
		splog.Info("Welcome to Stackit!")
	}
	splog.Newline()

	coloredTrunk := style.ColorBranchName(trunkName, false)
	splog.Info("Trunk set to %s", coloredTrunk)

	if isReset {
		splog.Info("All branches have been untracked")
	} else {
		splog.Info("Stackit initialized successfully!")
	}

	splog.Newline()
	splog.Info("Default configuration:")
	splog.Info("  - branch.pattern: %s", style.ColorDim(config.DefaultBranchPattern.String()))
	splog.Info("  - submit.footer:  %s", style.ColorDim("true"))
	splog.Info("  - undo.depth:     %s", style.ColorDim("10"))
	splog.Newline()
	splog.Info("Run '%s' to change these settings.", style.ColorCyan("stackit config"))

	splog.Newline()
	splog.Info("Pro-tip: enhance your workflow with integrations:")
	splog.Info("  - GitHub:     %s", style.ColorGreen("stackit github install"))
	splog.Info("  - Pre-commit: %s", style.ColorGreen("stackit precommit install"))
	splog.Info("  - Agents:     %s", style.ColorGreen("stackit agents install"))
}

// EnsureInitialized initializes stackit if not already initialized.
// Returns the repo root path. This is used by commands that need stackit
// to be initialized but want to auto-initialize for convenience.
func EnsureInitialized(ctx context.Context, writer io.Writer) (string, error) {
	runner := git.NewRunner()
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}

	cfg, _ := config.LoadConfig(repoRoot)
	if !cfg.IsInitialized() {
		splog := tui.NewSplogToWriter(writer)
		splog.Info("Stackit has not been initialized, attempting to setup now...")

		handler := &cliInitHandler{noInteractive: true, writer: writer}
		err := initaction.Action(ctx, repoRoot, initaction.Options{}, handler)
		if err != nil {
			return "", err
		}
	}

	return repoRoot, nil
}

// newInitCmd creates the init command
func newInitCmd() *cobra.Command {
	var (
		trunk         string
		reset         bool
		noInteractive bool
	)

	cmd := &cobra.Command{
		Use:          "init",
		Aliases:      []string{"i"},
		Short:        "Initialize Stackit in the current repository",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("failed to get repo root: %w", err)
			}

			handler := &cliInitHandler{noInteractive: noInteractive, writer: cmd.OutOrStdout()}
			opts := initaction.Options{
				Trunk: trunk,
				Reset: reset,
			}

			return initaction.Action(cmd.Context(), repoRoot, opts, handler)
		},
	}

	cmd.Flags().StringVar(&trunk, "trunk", "", "The name of your trunk branch")
	cmd.Flags().BoolVar(&reset, "reset", false, "Untrack all branches")
	cmd.Flags().BoolVar(&noInteractive, "no-interactive", false, "Disable interactive prompts")

	return cmd
}
