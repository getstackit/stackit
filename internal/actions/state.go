package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
)

// StateOptions configures the state command.
type StateOptions struct {
	JSON bool
}

// StateResult is a complete machine-readable snapshot of the repository's
// stack, working tree, and any in-progress operation. Agents read this single
// result instead of fanning out across separate branch/git-status/log/info
// calls, and it removes brittle scraping of `.git` for conflict state.
type StateResult struct {
	CurrentBranch string            `json:"current_branch"` // "" when detached
	Detached      bool              `json:"detached"`
	Trunk         string            `json:"trunk"`
	WorkingTree   WorkingTreeStatus `json:"working_tree"`
	Operation     OperationStatus   `json:"operation"`
	Stack         LogJSONResult     `json:"stack"`
}

// WorkingTreeStatus reports the staged/unstaged/untracked state of the worktree.
type WorkingTreeStatus struct {
	Clean     bool `json:"clean"`
	Staged    bool `json:"staged"`
	Unstaged  bool `json:"unstaged"`
	Untracked bool `json:"untracked"`
}

// OperationStatus reports an in-progress git/stackit operation and its conflicts.
type OperationStatus struct {
	// Kind is "none", "rebase", or "merge".
	Kind string `json:"kind"`
	// InProgress is true when a rebase or merge is mid-flight.
	InProgress bool `json:"in_progress"`
	// StackitHalted is true when a `stackit continue` is pending (a stackit
	// operation was interrupted by a conflict).
	StackitHalted bool `json:"stackit_halted"`
	// ConflictedFiles lists unmerged paths (always present, possibly empty).
	ConflictedFiles []string `json:"conflicted_files"`
}

// StateAction prints a complete snapshot of the stack and working tree.
func StateAction(ctx *app.Context, opts StateOptions) error {
	result := BuildState(ctx, opts)

	if opts.JSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		ctx.Output.Info("%s", string(data))
		return nil
	}

	printStateHuman(ctx, result)
	return nil
}

// BuildState gathers the snapshot without printing it.
func BuildState(ctx *app.Context, _ StateOptions) StateResult {
	eng := ctx.Engine

	result := StateResult{
		Trunk: eng.Trunk().GetName(),
		Stack: BuildLogJSON(ctx, LogOptions{Style: LogStyleNormal, JSON: true}),
	}

	if cur := eng.CurrentBranch(); cur != nil {
		result.CurrentBranch = cur.GetName()
	} else {
		result.Detached = true
	}

	// Working-tree state via a single git status --porcelain call.
	// A failed check is non-fatal; log and avoid reporting a clean tree.
	staged, unstaged, untracked, statusErr := eng.GetWorkingTreeStatus(ctx.Context)
	clean := !staged && !unstaged && !untracked
	if statusErr != nil {
		ctx.Output.Debug("state: working-tree status check failed: %v", statusErr)
		clean = false
	}
	result.WorkingTree = WorkingTreeStatus{
		Staged:    staged,
		Unstaged:  unstaged,
		Untracked: untracked,
		Clean:     clean,
	}

	// In-progress operation + conflicts.
	op := OperationStatus{Kind: "none", ConflictedFiles: []string{}}
	switch {
	case eng.IsRebaseInProgress(ctx.Context):
		op.Kind = "rebase"
		op.InProgress = true
	case eng.IsMergeInProgress(ctx.Context):
		op.Kind = "merge"
		op.InProgress = true
	}
	if op.InProgress {
		files, err := eng.GetUnmergedFiles(ctx.Context)
		if err != nil {
			ctx.Output.Debug("state: failed to list unmerged files: %v", err)
		} else {
			op.ConflictedFiles = files
		}
	}
	// A pending continuation file means a stackit operation halted on a conflict.
	if _, err := config.GetContinuationState(ctx.RepoRoot); err == nil {
		op.StackitHalted = true
	}
	result.Operation = op

	return result
}

func printStateHuman(ctx *app.Context, r StateResult) {
	out := ctx.Output

	if r.Detached {
		out.Info("HEAD detached (trunk: %s)", r.Trunk)
	} else {
		out.Info("On %s (trunk: %s)", r.CurrentBranch, r.Trunk)
	}

	if r.WorkingTree.Clean {
		out.Info("Working tree: clean")
	} else {
		var parts []string
		if r.WorkingTree.Staged {
			parts = append(parts, "staged")
		}
		if r.WorkingTree.Unstaged {
			parts = append(parts, "unstaged")
		}
		if r.WorkingTree.Untracked {
			parts = append(parts, "untracked")
		}
		out.Info("Working tree: %s changes", strings.Join(parts, ", "))
	}

	if r.Operation.InProgress {
		out.Info("%s in progress — %d conflicted file(s); resolve and run `stackit continue`",
			r.Operation.Kind, len(r.Operation.ConflictedFiles))
		for _, f := range r.Operation.ConflictedFiles {
			out.Info("    %s", f)
		}
	}

	out.Newline()
	if r.Stack.Summary.TotalBranches == 0 {
		out.Info("No tracked branches.")
		return
	}

	out.Info("Stack (%d branches):", r.Stack.Summary.TotalBranches)
	for _, b := range r.Stack.Branches {
		if b.IsTrunk {
			continue
		}
		marker := "  "
		if b.IsCurrent {
			marker = "▸ "
		}
		line := marker + b.Name
		if b.PR != nil {
			line += fmt.Sprintf("  #%d %s", b.PR.Number, b.PR.State)
			if b.PR.CIStatus != "" {
				line += " ci:" + b.PR.CIStatus
			}
		} else {
			line += "  (no PR)"
		}
		var flags []string
		if b.NeedsRestack {
			flags = append(flags, "needs-restack")
		}
		if b.IsLocked {
			flags = append(flags, "locked")
		}
		if b.IsFrozen {
			flags = append(flags, "frozen")
		}
		if len(flags) > 0 {
			line += "  [" + strings.Join(flags, ", ") + "]"
		}
		out.Info("%s", line)
	}
}
