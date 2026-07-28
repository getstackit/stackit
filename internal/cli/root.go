// Package cli provides command-line interface definitions using Cobra,
// including all subcommands and their flag definitions.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/cli/branch"
	"github.com/getstackit/stackit/internal/cli/integrations"
	"github.com/getstackit/stackit/internal/cli/navigation"
	"github.com/getstackit/stackit/internal/cli/shell"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/cli/worktree"
	"github.com/getstackit/stackit/internal/utils"
)

// NewRootCmd creates the root cobra command
func NewRootCmd(version, commit, date string) *cobra.Command {
	var (
		cwd           string
		debug         bool
		interactive   bool
		noInteractive bool
		verify        bool
		noVerify      bool
		quiet         bool
	)

	rootCmd := &cobra.Command{
		Use:          "stackit",
		Aliases:      []string{"st"},
		Short:        "Stackit is a command line tool that makes working with stacked changes fast & intuitive",
		Version:      version,
		SilenceUsage: true,
		Long: `Stackit is a command line tool that makes working with stacked changes fast & intuitive.

https://github.com/getstackit/stackit

Version: ` + version + `
Commit:  ` + commit + `
		Date:    ` + date,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve interactivity. Precedence:
			//   1. --no-interactive / --quiet           -> off
			//   2. explicit --interactive               -> honored as set
			//   3. STACKIT_NO_INTERACTIVE env, or no TTY -> off
			// This lets agents, CI, and pipes run without repeating
			// --no-interactive on every command, while explicit flags always win.
			switch {
			case noInteractive || quiet:
				interactive = false
			case cmd.Flags().Changed("interactive"):
				// honor the explicit --interactive value already in `interactive`
			case utils.NonInteractiveEnv() || !utils.TerminalDetected():
				interactive = false
			}

			if noVerify {
				verify = false
			}

			// Sync the boolean values back to the flags so common.GetGlobalOptions works
			if !interactive {
				_ = cmd.Flags().Set("interactive", "false")
			}
			if !verify {
				_ = cmd.Flags().Set("verify", "false")
			}

			return nil
		},
	}

	pf := rootCmd.PersistentFlags()
	pf.StringVar(&cwd, "cwd", "", "Working directory in which to perform operations.")
	pf.BoolVar(&debug, "debug", false, "Write debug output to the terminal.")
	pf.BoolVar(&interactive, "interactive", true, "Enable interactive features like prompts, pagers, and editors.")
	pf.BoolVar(&noInteractive, "no-interactive", false, "Disable interactive features.")
	pf.BoolVar(&verify, "verify", true, "Enable git and stackit hooks.")
	pf.BoolVar(&noVerify, "no-verify", false, "Disable git and stackit hooks.")
	pf.BoolVarP(&quiet, "quiet", "q", false, "Minimize output to the terminal. Implies --no-interactive.")

	rootCmd.AddCommand(newAbortCmd())
	rootCmd.AddCommand(newAddCmd())
	rootCmd.AddCommand(branch.NewAbsorbCmd())
	rootCmd.AddCommand(integrations.NewAgentsCmd(version))
	rootCmd.AddCommand(navigation.NewBottomCmd())
	rootCmd.AddCommand(navigation.NewCheckoutCmd())
	rootCmd.AddCommand(newCherryPickCmd())
	rootCmd.AddCommand(navigation.NewChildrenCmd())
	rootCmd.AddCommand(stack.NewFlattenCmd())
	rootCmd.AddCommand(newContinueCmd())
	rootCmd.AddCommand(branch.NewCreateCmd())
	rootCmd.AddCommand(newDebugCmd())
	rootCmd.AddCommand(newDescribeCmd())
	rootCmd.AddCommand(newDocsCmd())
	rootCmd.AddCommand(branch.NewDeleteCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(navigation.NewDownCmd())
	rootCmd.AddCommand(branch.NewFoldCmd())
	rootCmd.AddCommand(stack.NewForeachCmd())
	rootCmd.AddCommand(branch.NewFreezeCmd())
	rootCmd.AddCommand(branch.NewGetCmd())
	rootCmd.AddCommand(newInfoCmd())
	rootCmd.AddCommand(newInitCmd(version))
	rootCmd.AddCommand(integrations.NewGithubCmd())
	rootCmd.AddCommand(branch.NewLockCmd())
	rootCmd.AddCommand(navigation.NewLogCmd())
	rootCmd.AddCommand(navigation.NewTreeCmd())
	rootCmd.AddCommand(navigation.NewTreeShortAliasCmd())
	rootCmd.AddCommand(navigation.NewMainCmd())
	rootCmd.AddCommand(stack.NewMergeCmd())
	rootCmd.AddCommand(branch.NewModifyCmd())
	rootCmd.AddCommand(stack.NewMoveCmd())
	rootCmd.AddCommand(navigation.NewParentCmd())
	rootCmd.AddCommand(branch.NewPopCmd())
	rootCmd.AddCommand(stack.NewPluckCmd())
	rootCmd.AddCommand(navigation.NewPrCmd())
	rootCmd.AddCommand(newRebaseCmd())
	rootCmd.AddCommand(branch.NewRenameCmd())
	rootCmd.AddCommand(stack.NewReorderCmd())
	rootCmd.AddCommand(newResetCmd())
	rootCmd.AddCommand(stack.NewRestackCmd())
	rootCmd.AddCommand(integrations.NewPrecommitCmd())
	rootCmd.AddCommand(integrations.NewPrepushCmd())
	rootCmd.AddCommand(branch.NewSplitCmd())
	rootCmd.AddCommand(branch.NewSquashCmd())
	rootCmd.AddCommand(newScopeCmd())
	rootCmd.AddCommand(navigation.NewShareCmd())
	rootCmd.AddCommand(shell.NewShellCmd())
	rootCmd.AddCommand(newStateCmd())
	rootCmd.AddCommand(stack.NewSubmitCmd())
	rootCmd.AddCommand(stack.NewSyncCmd())
	rootCmd.AddCommand(navigation.NewTopCmd())
	rootCmd.AddCommand(newUICmd())
	rootCmd.AddCommand(newTrackCmd())
	rootCmd.AddCommand(newUntrackCmd())
	rootCmd.AddCommand(navigation.NewTrunkCmd())
	rootCmd.AddCommand(newUndoCmd())
	rootCmd.AddCommand(navigation.NewUpCmd())
	rootCmd.AddCommand(branch.NewUnfreezeCmd())
	rootCmd.AddCommand(branch.NewUnlockCmd())
	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(worktree.NewWorktreeCmd())

	rootCmd.AddCommand(stack.NewSsCmd())

	return rootCmd
}
