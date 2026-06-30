package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/components/tree"
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

// StackInfoOutput represents JSON-serializable output for the stack info command
type StackInfoOutput struct {
	StackTitle       string            `json:"stack_title,omitempty"`
	StackDescription string            `json:"stack_description,omitempty"`
	Branches         []StackBranchInfo `json:"branches"`
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
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	stackBranches := graph.Range(*currentBranch, engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})
	result := make([]StackBranchInfo, 0, len(stackBranches))

	// Read each per-branch concern the loop needs as its own batched value map —
	// commits, diff stats, changed-file counts, and PR submission status — instead
	// of warming an engine-global cache. The loop is a projection over these maps.
	commits := eng.BatchCommits(stackBranches, engine.CommitFormatReadable)
	diffs := eng.BatchDiffStats(stackBranches)
	fileCounts := eng.BatchChangedFileCounts(ctx.Context, stackBranches)

	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	defer cancelRemote()
	prStatuses, _ := eng.BatchGetPRSubmissionStatus(remoteCtx, stackBranches)

	for _, branch := range stackBranches {
		if branch.IsTrunk() {
			continue
		}
		name := branch.GetName()

		diff := diffs[name]
		info := StackBranchInfo{
			Name:           name,
			Parent:         branch.GetParentOrTrunk(),
			IsLocked:       branch.IsLocked(),
			IsFrozen:       branch.IsFrozen(),
			Scope:          branch.GetScope().String(),
			CommitMessages: commits[name],
			DiffStats: DiffStats{
				Additions:    diff.Added,
				Deletions:    diff.Deleted,
				FilesChanged: fileCounts[name],
			},
		}
		if info.CommitMessages == nil {
			info.CommitMessages = []string{}
		}

		// PR info
		prStatus := prStatuses[name]
		if prStatus.PRNumber != nil {
			info.PRNumber = prStatus.PRNumber
			if prStatus.PRInfo != nil {
				info.PRURL = prStatus.PRInfo.URL()
			}
		}

		result = append(result, info)
	}

	if opts.JSON {
		output := StackInfoOutput{
			Branches: result,
		}
		stackDesc := eng.GetStackDescription(*currentBranch)
		if stackDesc != nil && !stackDesc.IsEmpty() {
			output.StackTitle = stackDesc.Title
			output.StackDescription = stackDesc.Description
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal stack info to JSON: %w", err)
		}
		ctx.Output.Info("%s", string(data))
	} else {
		// Show stack description if present
		stackDesc := eng.GetStackDescription(*currentBranch)
		if stackDesc != nil && !stackDesc.IsEmpty() {
			// Render title and description together through glamour for consistent formatting
			var markdown string
			if stackDesc.Description != "" {
				markdown = "# " + stackDesc.Title + "\n\n" + stackDesc.Description
			} else {
				markdown = "# " + stackDesc.Title
			}
			rendered := output.RenderMarkdown(markdown)
			ctx.Output.Info("%s", rendered)
			ctx.Output.Info("")
			ctx.Output.Info(strings.Repeat("─", 40))
			ctx.Output.Info("")
		}

		// Build tree data structure for rendering
		trunkName := eng.Trunk().GetName()
		stackTree := tree.NewStackTree(stackBranches, currentBranch.GetName(), trunkName)
		stackTree.FixedMap = make(map[string]bool)
		// Resolve restack status for the whole stack in one batched parent-revision
		// read instead of a per-branch NeedsRestack() (each of which would shell a
		// separate `git rev-parse` for the parent).
		statuses := eng.ReadBranchStatuses(stackBranches)
		for _, branch := range stackBranches {
			// IsFixed means it does NOT need restack
			stackTree.FixedMap[branch.GetName()] = statuses.IsUpToDate(branch)
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
