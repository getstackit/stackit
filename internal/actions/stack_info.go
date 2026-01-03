package actions

import (
	"encoding/json"
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/style"
)

// StackBranchInfo represents JSON-serializable info for a single branch in a stack
type StackBranchInfo struct {
	Name           string    `json:"name"`
	Parent         string    `json:"parent"`
	IsLocked       bool      `json:"is_locked"`
	IsFrozen       bool      `json:"is_frozen"`
	Scope          string    `json:"scope"`
	CommitMessages []string  `json:"commit_messages"`
	DiffStats      DiffStats `json:"diff_stats"`
}

// DiffStats represents summary diff information
type DiffStats struct {
	FilesChanged int `json:"files_changed"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

// StackInfoOptions contains options for the stack info logic
type StackInfoOptions struct {
	JSON bool
}

// StackInfoAction retrieves information about branches in the current stack
func StackInfoAction(ctx *app.Context, opts StackInfoOptions) error {
	eng := ctx.Engine

	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
	}

	stackBranches := eng.GetFullStack(*currentBranch)
	result := make([]StackBranchInfo, 0, len(stackBranches))

	for _, branch := range stackBranches {
		if branch.IsTrunk() {
			continue
		}

		info := StackBranchInfo{
			Name:           branch.GetName(),
			Parent:         branch.GetParentPrecondition(),
			IsLocked:       branch.IsLocked(),
			IsFrozen:       branch.IsFrozen(),
			Scope:          branch.GetScope().String(),
			CommitMessages: []string{},
		}

		// Commit messages
		commits, err := branch.GetAllCommits(engine.CommitFormatReadable)
		if err == nil {
			info.CommitMessages = commits
		}

		// Diff stats
		added, deleted, err := branch.GetDiffStats()
		if err == nil {
			info.DiffStats.Additions = added
			info.DiffStats.Deletions = deleted
		}

		// Files changed
		parentName := branch.GetParentPrecondition()
		parentRev, err := eng.GetRevision(eng.GetBranch(parentName))
		if err == nil {
			branchRev, err := branch.GetRevision()
			if err == nil {
				files, err := eng.GetChangedFiles(ctx.Context, parentRev, branchRev)
				if err == nil {
					info.DiffStats.FilesChanged = len(files)
				}
			}
		}

		result = append(result, info)
	}

	if opts.JSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal stack info to JSON: %w", err)
		}
		ctx.Splog.Info("%s", string(data))
	} else {
		currentBranchName := ""
		if currentBranch != nil {
			currentBranchName = currentBranch.GetName()
		}

		for i, info := range result {
			coloredName := style.ColorBranchName(info.Name, info.Name == currentBranchName)
			ctx.Splog.Info("%s", coloredName)

			var coloredParent string
			if info.Parent != "" {
				parentBranch := eng.GetBranch(info.Parent)
				coloredParent = style.ColorBranchNameWithTrunk(info.Parent, false, eng.IsTrunk(parentBranch))
			} else {
				coloredParent = style.ColorDim("(none)")
			}
			ctx.Splog.Info("  %s %s", style.ColorCyan("Parent:"), coloredParent)

			if info.IsLocked {
				ctx.Splog.Info("  %s %s", style.IconLocked(), style.ColorDim("(locked)"))
			}
			if info.IsFrozen {
				ctx.Splog.Info("  %s %s", style.IconFrozen(), style.ColorDim("(frozen)"))
			}
			if info.Scope != "" {
				ctx.Splog.Info("  %s %s", style.ColorCyan("Scope:"), style.ColorScope(info.Scope))
			}

			ctx.Splog.Info("  %s %d", style.ColorCyan("Commits:"), len(info.CommitMessages))
			ctx.Splog.Info("  %s +%d -%d in %d files", style.ColorCyan("Changes:"), info.DiffStats.Additions, info.DiffStats.Deletions, info.DiffStats.FilesChanged)

			if i < len(result)-1 {
				ctx.Splog.Newline()
			}
		}
	}

	return nil
}
