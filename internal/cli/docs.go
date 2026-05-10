package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/config"
)

// newDocsCmd creates the docs command for generating documentation.
// This is a hidden command used by the documentation build process.
func newDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate documentation (internal use)",
		Hidden: true,
	}

	cmd.AddCommand(newDocsConfigCmd())
	cmd.AddCommand(newDocsTemplateCmd())
	cmd.AddCommand(newDocsYAMLExampleCmd())

	return cmd
}

// newDocsConfigCmd creates the docs config subcommand.
func newDocsConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Generate config option documentation (markdown)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			docs := config.GenerateConfigDocs()
			_, err := fmt.Fprint(cmd.OutOrStdout(), docs)
			return err
		},
	}
}

// newDocsTemplateCmd creates the docs template subcommand.
func newDocsTemplateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "template",
		Short: "Generate .stackit.yaml template",
		RunE: func(cmd *cobra.Command, _ []string) error {
			template := config.GenerateConfigTemplate()
			_, err := fmt.Fprint(cmd.OutOrStdout(), template)
			return err
		},
	}
}

// newDocsYAMLExampleCmd creates the docs yaml-example subcommand.
func newDocsYAMLExampleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "yaml-example",
		Short: "Generate .stackit.yaml example with common settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			example := config.GenerateYAMLExample()
			_, err := fmt.Fprint(cmd.OutOrStdout(), example)
			return err
		},
	}
}
