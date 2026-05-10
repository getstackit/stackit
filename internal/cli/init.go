package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	initaction "github.com/getstackit/stackit/internal/actions/init"
	"github.com/getstackit/stackit/internal/cli/integrations"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/rerere"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// cliInitHandler implements initaction.Handler for the CLI
type cliInitHandler struct {
	noInteractive bool
	writer        io.Writer
	version       string
	runner        git.Runner
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
	splog := output.NewConsoleOutput(h.writer, false)

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

	if !h.noInteractive && tui.IsTTY() {
		if _, err := rerere.EnsureEnabled(context.Background(), h.runner, true, nil); err != nil {
			splog.Warn("Failed to enable git rerere: %v", err)
		}
	}

	// Offer interactive integration installation
	if !h.noInteractive && tui.IsTTY() {
		h.offerIntegrations(splog)
	} else {
		// Non-interactive: just show hints
		splog.Newline()
		splog.Info("Pro-tip: enhance your workflow with integrations:")
		splog.Info("  - GitHub:     %s", style.ColorGreen("stackit github install"))
		splog.Info("  - Pre-commit: %s", style.ColorGreen("stackit precommit install"))
		splog.Info("  - Agents:     %s", style.ColorGreen("stackit agents install"))
	}
}

// offerIntegrations prompts the user to install integrations interactively
func (h *cliInitHandler) offerIntegrations(splog output.Output) {
	// Check which integrations are already installed
	githubInstalled := integrations.IsGitHubInstalled(h.runner)
	precommitInstalled := integrations.IsPrecommitInstalled(h.runner)
	agentsInstalled := integrations.IsAgentsInstalled(h.runner)

	// If all are installed, skip the integration prompts
	if githubInstalled && precommitInstalled && agentsInstalled {
		splog.Newline()
		splog.Info("All integrations already installed.")
		return
	}

	splog.Newline()
	splog.Info("Would you like to install any integrations?")
	splog.Newline()

	// Offer GitHub integration (skip if already installed)
	if githubInstalled {
		splog.Info("✓ GitHub Actions workflow already installed")
	} else {
		installGitHub, err := tui.PromptConfirm("Install GitHub Actions workflow? (CI checks for stacked PRs)", true)
		if err == nil && installGitHub {
			if err := integrations.InstallGitHub(h.runner, false, h.writer); err != nil {
				splog.Warn("Failed to install GitHub integration: %v", err)
			}
		}
	}

	// Offer pre-commit hook (skip if already installed)
	if precommitInstalled {
		splog.Info("✓ Pre-commit hook already installed")
	} else {
		installPrecommit, err := tui.PromptConfirm("Install pre-commit hook? (Prevents commits on locked branches)", true)
		if err == nil && installPrecommit {
			if err := integrations.InstallPrecommit(h.runner, h.writer); err != nil {
				splog.Warn("Failed to install pre-commit hook: %v", err)
			}
		}
	}

	// Offer agent integration (skip if already installed)
	if agentsInstalled {
		splog.Info("✓ AI agent files already installed")
	} else {
		installAgents, err := tui.PromptConfirm("Install AI agent files? (Claude Code / Codex integration)", true)
		if err == nil && installAgents {
			if err := integrations.InstallAgents(h.runner, false, h.version, h.writer); err != nil {
				splog.Warn("Failed to install agent files: %v", err)
			}
		}
	}
}

// EnsureInitialized initializes stackit if not already initialized.
// Returns the repo root path. This is used by commands that need stackit
// to be initialized but want to auto-initialize for convenience.
func EnsureInitialized(ctx context.Context, writer io.Writer) (string, error) {
	runner := git.NewRunner(nil)
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}

	cfg, _ := config.LoadConfig(repoRoot)
	if !cfg.IsInitialized() {
		splog := output.NewConsoleOutput(writer, false)
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
func newInitCmd(version string) *cobra.Command {
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
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("failed to get repo root: %w", err)
			}
			interactive, _ := cmd.Flags().GetBool("interactive")
			quiet, _ := cmd.Flags().GetBool("quiet")

			handler := &cliInitHandler{
				noInteractive: noInteractive || !interactive || quiet,
				writer:        cmd.OutOrStdout(),
				version:       version,
				runner:        runner,
			}
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
