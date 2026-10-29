package restack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// RebaseStateReader is the engine surface ConflictReporter reads.
type RebaseStateReader interface {
	engine.BranchLookup
	GetUnmergedFiles(ctx context.Context) ([]string, error)
	GetRebaseHead(ctx context.Context) (string, error)
}

// ConflictReporter prints where a conflicted rebase stopped and how to finish
// it.
type ConflictReporter struct {
	Engine   RebaseStateReader
	Out      Printer
	RepoRoot string
}

// Print displays conflict information and instructions to the user. It
// assumes a rebase is in progress: HEAD-side markers reflect the parent/base
// and the ">>>>>>>" side reflects the branch being replayed.
func (r *ConflictReporter) Print(ctx context.Context, branchName string) error {
	out := r.Out

	out.Info("%s", output.Red(fmt.Sprintf("Hit conflict restacking %s", branchName)))
	out.Newline()

	unmergedFiles, err := r.Engine.GetUnmergedFiles(ctx)
	if err == nil && len(unmergedFiles) > 0 {
		out.Info("%s", output.Yellow("Conflicted files:"))
		for _, file := range unmergedFiles {
			sections, sectionErr := conflictMarkerSectionsForFile(r.RepoRoot, file)
			if sectionErr != nil || len(sections) == 0 {
				out.Info("  %s", output.Red(file))
				continue
			}
			for _, section := range sections {
				out.Info("  %s (lines %d-%d)", output.Red(file), section.StartLine, section.EndLine)
			}
		}
		out.Newline()
	}

	rebaseHead, err := r.Engine.GetRebaseHead(ctx)
	if err == nil && rebaseHead != "" {
		out.Info("%s", output.Yellow(fmt.Sprintf("You are here (resolving %s):", git.ShortRevision(rebaseHead, 0))))
		out.Newline()
	}

	parentBranch := "the current base"
	if branch := r.Engine.GetBranch(branchName); branch.Name() != "" {
		parentBranch = branch.ParentOrTrunk()
	}

	out.Info("%s", output.Yellow("To resolve:"))
	out.Info("  1. Open each conflicted file and remove conflict markers:")
	out.Info("     %s  (incoming changes from %s)", output.Cyan("<<<<<<< HEAD"), parentBranch)
	out.Info("     %s", output.Cyan("======="))
	out.Info("     %s  (changes from %s)", output.Cyan(">>>>>>>"), branchName)
	out.Info("  2. Stage resolved files: %s", output.Cyan("stackit add <file>"))
	out.Info("  3. Continue the previous Stackit command: %s", output.Cyan("stackit continue"))
	out.Info("  4. Abort and restore the pre-command snapshot: %s", output.Cyan("stackit abort"))
	out.Info("Tip: %s stages all resolved files before continuing.", output.Cyan("stackit continue --all"))

	return nil
}

type conflictMarkerSection struct {
	StartLine int
	EndLine   int
}

func conflictMarkerSectionsForFile(repoRoot, file string) ([]conflictMarkerSection, error) {
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, file)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return findConflictMarkerSections(string(content)), nil
}

// findConflictMarkerSections scans for git conflict regions delimited by
// <<<<<<<, =======, and >>>>>>> markers. Diff3-style ||||||| ancestor markers
// between <<<<<<< and ======= are tolerated (ignored). A new <<<<<<< before a
// section is closed restarts tracking, so unterminated sections do not block
// detection of subsequent complete sections.
func findConflictMarkerSections(content string) []conflictMarkerSection {
	sections := make([]conflictMarkerSection, 0)
	startLine := 0
	seenSeparator := false

	for i, line := range strings.Split(content, "\n") {
		lineNo := i + 1
		switch {
		case strings.HasPrefix(line, "<<<<<<<"):
			startLine = lineNo
			seenSeparator = false
		case startLine > 0 && strings.HasPrefix(line, "======="):
			seenSeparator = true
		case startLine > 0 && seenSeparator && strings.HasPrefix(line, ">>>>>>>"):
			sections = append(sections, conflictMarkerSection{StartLine: startLine, EndLine: lineNo})
			startLine = 0
			seenSeparator = false
		}
	}

	return sections
}
