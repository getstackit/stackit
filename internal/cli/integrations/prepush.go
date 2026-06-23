package integrations

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/integrations"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewPrepushCmd creates the prepush command
func NewPrepushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "prepush",
		Short:        "Manage and run pre-push hooks",
		Long:         `Manage and run git pre-push hooks that prevent pushing locked branches.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newPrepushInstallCmd())
	cmd.AddCommand(newPrepushUninstallCmd())
	cmd.AddCommand(newPrepushVerifyCmd())

	return cmd
}

func newPrepushInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          cmdInstall,
		Short:        "Install the pre-push hook into the current repository",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrepushInstallAction)
		},
	}
}

func newPrepushUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "uninstall",
		Short:        "Uninstall the pre-push hook from the current repository",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrepushUninstallAction)
		},
	}
}

func newPrepushVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify that branches being pushed are not locked or frozen",
		Long: `Verify that branches being pushed are not locked or frozen.
Reads refs from stdin in git pre-push hook format.
Exits with a non-zero exit code if any branch is restricted.`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, integrations.PrepushVerifyAction)
		},
	}
}
