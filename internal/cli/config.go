package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/tui"
	configtui "stackit.dev/stackit/internal/tui/config"
)

// newConfigCmd creates the config command
func newConfigCmd() *cobra.Command {
	var listFlag bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get and set repository configuration",
		Long: `Get and set repository configuration values.

When run without subcommands, opens an interactive TUI for editing configuration.
Use --list to print all configuration values instead.

Examples:
  stackit config                    # Interactive TUI
  stackit config --list             # Print all config values
  stackit config get branch.pattern
  stackit config set branch.pattern "{username}/{date}/{message}"
  stackit config get submit.footer
  stackit config set submit.footer false`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			// If --list flag is set, or terminal is not interactive, show list
			if listFlag || !tui.IsTTY() {
				return actions.ConfigListAction(repoRoot, cmd.OutOrStdout())
			}

			// Otherwise, show interactive TUI
			return configtui.TUIAction(repoRoot)
		},
	}

	cmd.Flags().BoolVarP(&listFlag, "list", "l", false, "Print all configuration values instead of opening interactive TUI")

	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())

	return cmd
}

// newConfigGetCmd creates the config get command
func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get <key>",
		Short:        "Get a configuration value",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			key := args[0]
			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			switch key {
			case "branch.pattern":
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.BranchNamePattern())
			case "submit.footer":
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.SubmitFooter())
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			return nil
		},
	}

	return cmd
}

// newConfigSetCmd creates the config set command
func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Set a configuration value",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			key := args[0]
			value := args[1]

			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			splog := tui.NewSplogToWriter(cmd.OutOrStdout())

			switch key {
			case "branch.pattern":
				if err := cfg.SetBranchNamePattern(value); err != nil {
					return fmt.Errorf("failed to set branch.pattern: %w", err)
				}
				if err := cfg.Save(); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}
				splog.Info("Set branch.pattern to: %s", value)
			case "submit.footer":
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for submit.footer: %s (must be 'true' or 'false')", value)
				}
				cfg.SetSubmitFooter(enabled)
				if err := cfg.Save(); err != nil {
					return fmt.Errorf("failed to save config: %w", err)
				}
				splog.Info("Set submit.footer to: %v", enabled)
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			return nil
		},
	}

	return cmd
}
