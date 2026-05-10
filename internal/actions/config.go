package actions

import (
	"fmt"
	"io"
	"strings"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

const valueNotSet = "(not set)"

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
		mergeMethod = valueNotSet
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

	// CI settings
	ciCommand := cfg.CICommand()
	if ciCommand == "" {
		ciCommand = valueNotSet
	}
	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("ci.command"), ciCommand))
	lines = append(lines, fmt.Sprintf("%s: %d", style.ColorCyan("ci.timeout"), cfg.CITimeout()))

	// Undo settings
	lines = append(lines, fmt.Sprintf("%s: %d", style.ColorCyan("undo.depth"), cfg.UndoStackDepth()))

	// Worktree settings
	worktreeBasePath := cfg.WorktreeBasePath()
	if worktreeBasePath == "" {
		worktreeBasePath = valueNotSet
	}
	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("worktree.basePath"), worktreeBasePath))
	lines = append(lines, fmt.Sprintf("%s: %v", style.ColorCyan("worktree.autoClean"), cfg.WorktreeAutoClean()))

	// Split settings
	lines = append(lines, fmt.Sprintf("%s: %s", style.ColorCyan("split.hunkSelector"), cfg.SplitHunkSelector()))

	// Concurrency settings
	lines = append(lines, fmt.Sprintf("%s: %d", style.ColorCyan("maxConcurrency"), cfg.MaxConcurrency()))

	out.Print(strings.Join(lines, "\n"))
	out.Newline()

	return nil
}
