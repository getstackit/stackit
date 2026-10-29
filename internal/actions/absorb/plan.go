// Package absorb provides functionality for absorbing staged changes into commits downstack.
package absorb

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// PlanJSON is the machine-readable absorb plan for LLM consumption
type PlanJSON struct {
	CurrentBranch string             `json:"current_branch"`
	Absorbed      []AbsorbedHunk     `json:"absorbed"`
	Unabsorbable  []UnabsorbableHunk `json:"unabsorbable"`
	NewFiles      []string           `json:"new_files"`
	Stack         []StackNode        `json:"stack"`
}

// AbsorbedHunk represents a hunk that was successfully absorbed
type AbsorbedHunk struct {
	File         string `json:"file"`
	Lines        string `json:"lines"`
	TargetBranch string `json:"target_branch"`
	TargetCommit string `json:"target_commit"`
	Content      string `json:"content"`
}

// UnabsorbableHunk represents a hunk that could not be absorbed
type UnabsorbableHunk struct {
	File    string `json:"file"`
	Lines   string `json:"lines"`
	Reason  string `json:"reason"`
	Content string `json:"content"`
}

// StackNode represents a branch in the stack structure
type StackNode struct {
	Name      string `json:"name"`
	Parent    string `json:"parent,omitempty"`
	IsTrunk   bool   `json:"is_trunk,omitempty"`
	IsCurrent bool   `json:"is_current,omitempty"`
}

// GeneratePlanJSON creates a machine-readable plan from absorb results
func GeneratePlanJSON(ctx context.Context,
	currentBranch string,
	hunkTargets []git.HunkTarget,
	unabsorbedHunks []Unabsorbable,
	newFiles []string,
	eng engine.Engine,
) ([]byte, error) {
	plan := PlanJSON{
		CurrentBranch: currentBranch,
		Absorbed:      make([]AbsorbedHunk, 0, len(hunkTargets)),
		Unabsorbable:  make([]UnabsorbableHunk, 0, len(unabsorbedHunks)),
		NewFiles:      newFiles,
		Stack:         buildStackNodes(ctx, eng, currentBranch),
	}

	// Convert absorbed hunks. Resolve all target branches in one batched scan.
	commitBranches := eng.FindBranchesForCommits(ctx, targetCommitSHAs(hunkTargets))
	for _, target := range hunkTargets {
		branchName := commitBranches[target.CommitSHA]
		if branchName == "" {
			branchName = "unknown"
		}

		plan.Absorbed = append(plan.Absorbed, AbsorbedHunk{
			File:         target.Hunk.File,
			Lines:        formatLines(target.Hunk.NewStart, target.Hunk.NewCount),
			TargetBranch: branchName,
			TargetCommit: target.CommitSHA,
			Content:      target.Hunk.Content,
		})
	}

	// Convert unabsorbable hunks. Binary and deleted-file hunks carry no
	// new-side line numbers, so derive the range the same way terminal output
	// does instead of reporting a bogus "0".
	for _, unabsorbable := range unabsorbedHunks {
		hunk := unabsorbable.Hunk
		start, end := hunkLineRange(hunk)
		plan.Unabsorbable = append(plan.Unabsorbable, UnabsorbableHunk{
			File:    hunk.File,
			Lines:   formatLines(start, end-start+1),
			Reason:  string(unabsorbable.Reason),
			Content: hunk.Content,
		})
	}

	return json.MarshalIndent(plan, "", "  ")
}

// buildStackNodes creates the stack structure from the engine
func buildStackNodes(ctx context.Context, eng engine.Engine, currentBranch string) []StackNode {
	var nodes []StackNode

	// Get all branches and build the stack
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	current := eng.GetBranch(currentBranch)
	if current.Name() == "" {
		return nodes
	}

	// Get the path from trunk to current branch
	branches := graph.Range(current, engine.StackRange{RecursiveParents: true, IncludeCurrent: true})

	// Also include trunk
	trunk := eng.Trunk()
	if trunk.Name() != "" {
		nodes = append(nodes, StackNode{
			Name:    trunk.Name(),
			IsTrunk: true,
		})
	}

	// Add branches in order (oldest to newest)
	for _, branch := range slices.Backward(branches) {
		if branch.IsTrunk() {
			continue // Already added trunk
		}

		parent := branch.Parent()
		parentName := ""
		if parent != nil {
			parentName = parent.Name()
		}

		nodes = append(nodes, StackNode{
			Name:      branch.Name(),
			Parent:    parentName,
			IsCurrent: branch.Name() == currentBranch,
		})
	}

	return nodes
}

// targetCommitSHAs returns the unique commit SHAs referenced by the hunk
// targets, preserving first-seen order, for a single batched branch lookup.
func targetCommitSHAs(targets []git.HunkTarget) []string {
	seen := make(map[string]struct{}, len(targets))
	shas := make([]string, 0, len(targets))
	for _, t := range targets {
		if _, ok := seen[t.CommitSHA]; ok {
			continue
		}
		seen[t.CommitSHA] = struct{}{}
		shas = append(shas, t.CommitSHA)
	}
	return shas
}

// formatLines formats line range as "start-end" or just "start" for single lines
func formatLines(start, count int) string {
	if count <= 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, start+count-1)
}
