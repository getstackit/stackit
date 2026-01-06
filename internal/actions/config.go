package actions

import (
	"fmt"
	"io"
	"strings"

	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui/style"
)

// ConfigListAction prints all configuration values in a formatted way
func ConfigListAction(repoRoot string, writer io.Writer) error {
	out := output.NewConsoleOutput(writer, false)

	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get trunk
	trunk := cfg.Trunk()

	// Get all trunks
	trunks := cfg.AllTrunks()

	// Get branch name pattern
	branchPattern := cfg.BranchNamePattern()

	// Get submit.footer
	submitFooter := cfg.SubmitFooter()

	// Get merge.method
	mergeMethod := cfg.MergeMethod()
	if mergeMethod == "" {
		mergeMethod = "(not set)"
	}

	// Format and print
	var lines []string
	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("trunk"), trunk))

	if len(trunks) > 1 {
		additionalTrunks := []string{}
		for _, t := range trunks {
			if t != trunk {
				additionalTrunks = append(additionalTrunks, t)
			}
		}
		if len(additionalTrunks) > 0 {
			lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("trunks"), strings.Join(additionalTrunks, ", ")))
		}
	}

	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("branch.pattern"), branchPattern))
	lines = append(lines, fmt.Sprintf("%s: %v", style.ColorCyan("submit.footer"), submitFooter))
	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("merge.method"), mergeMethod))

	out.Print(strings.Join(lines, "\n"))
	out.Newline()

	return nil
}
