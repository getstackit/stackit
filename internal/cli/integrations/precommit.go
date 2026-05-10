package integrations

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/integrations"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewPrecommitCmd creates the precommit command
func NewPrecommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "precommit",
		Short:        "Manage and run pre-commit hooks",
		Long:         `Manage and run git pre-commit hooks that validate Stackit state.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newPrecommitInstallCmd())
	cmd.AddCommand(newPrecommitUninstallCmd())
	cmd.AddCommand(newPrecommitVerifyCmd())

	return cmd
}

func newPrecommitInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "install",
		Short:        "Install the pre-commit hook into the current repository",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrecommitInstallAction)
		},
	}
}

func newPrecommitUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "uninstall",
		Short:        "Uninstall the pre-commit hook from the current repository",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrecommitUninstallAction)
		},
	}
}

func newPrecommitVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify that the current branch is not locked or frozen",
		Long: `Verify that the current branch is not locked or frozen.
Exits with a non-zero exit code if the branch is restricted.`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrecommitVerifyAction)
		},
	}
}
