package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/tui/components/tree"
)

// StackBranchInfo represents JSON-serializable info for a single branch in a stack
type StackBranchInfo struct {
	Name           string    `json:"name"`
	Parent         string    `json:"parent"`
	IsLocked       bool      `json:"is_locked"`
	IsFrozen       bool      `json:"is_frozen"`
	Scope          string    `json:"scope"`
	PRNumber       *int      `json:"pr_number,omitempty"`
	PRURL          string    `json:"pr_url,omitempty"`
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
		return errors.ErrNotOnBranch
	}

	// Build StackGraph for efficient traversals
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	stackBranches := graph.Range(*currentBranch, engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})
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

		// PR info
		prStatus, err := branch.GetPRSubmissionStatus()
		if err == nil && prStatus.PRNumber != nil {
			info.PRNumber = prStatus.PRNumber
			if prStatus.PRInfo != nil {
				info.PRURL = prStatus.PRInfo.URL()
			}
		}

		result = append(result, info)
	}

	if opts.JSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal stack info to JSON: %w", err)
		}
		ctx.Output.Info("%s", string(data))
	} else {
		// Build tree data structure for rendering
		trunkName := eng.Trunk().GetName()
		stackTree := tree.NewStackTree(stackBranches, currentBranch.GetName(), trunkName)
		stackTree.FixedMap = make(map[string]bool)
		for _, branch := range stackBranches {
			// IsFixed means it does NOT need restack
			stackTree.FixedMap[branch.GetName()] = !branch.NeedsRestack()
		}
		stackTree.FixedMap[trunkName] = true

		renderer := tree.NewRenderer(stackTree)

		// Build annotations with commit messages and PR info
		annotations := make(map[string]tree.BranchAnnotation)
		for _, info := range result {
			annotations[info.Name] = tree.BranchAnnotation{
				CommitMessages: info.CommitMessages,
				LinesAdded:     info.DiffStats.Additions,
				LinesDeleted:   info.DiffStats.Deletions,
				Scope:          info.Scope,
				IsLocked:       info.IsLocked,
				IsFrozen:       info.IsFrozen,
				PRNumber:       info.PRNumber,
				PRURL:          info.PRURL,
			}
		}
		renderer.SetAnnotations(annotations)

		// Render the tree with commit messages
		lines := renderer.RenderStack(currentBranch.GetName(), tree.RenderOptions{
			ShowCommitMessages:  true,
			HideSummary:         true,
			SkipSelectionPrefix: true,
		})

		ctx.Output.Info("%s", strings.Join(lines, "\n"))
	}

	return nil
}
