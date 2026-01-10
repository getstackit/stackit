package cli

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/doctor"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/config"
)

// newDoctorCmd creates the doctor command
func newDoctorCmd() *cobra.Command {
	var fix bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common issues with your stackit setup",
		Long: `Run diagnostic checks on your stackit environment and repository.

The doctor command checks:
  - Environment: Git version, GitHub CLI, and authentication
  - Repository: Git repository status, remote configuration, and trunk branch
  - Stack State: Metadata integrity, cycle detection, and missing parent branches`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Get config values
				cfg, _ := config.LoadConfig(ctx.RepoRoot)
				trunk := cfg.Trunk()

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewDoctorUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// Run doctor action
				return doctor.Action(ctx, doctor.Options{
					Fix:   fix,
					Trunk: trunk,
				}, handler)
			})
		},
	}

	cmd.Flags().BoolVar(&fix, "fix", false, "Attempt to automatically fix any issues found")

	return cmd
}
