package merge

import (
	"encoding/json"
	"fmt"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/shippable"
)

type statusJSONBlocking struct {
	Branch   string                   `json:"branch"`
	PRNumber int                      `json:"pr_number,omitempty"`
	Reason   shippable.BlockingReason `json:"reason"`
}

type statusJSONStack struct {
	RootBranch  string               `json:"root_branch"`
	AllBranches []string             `json:"all_branches"`
	PRCount     int                  `json:"pr_count"`
	Scope       string               `json:"scope,omitempty"`
	Status      shippable.Status     `json:"status"`
	Author      string               `json:"author,omitempty"`
	PRTitle     string               `json:"pr_title,omitempty"`
	ApprovalOK  bool                 `json:"approval_ok"`
	GitHubCIOK  bool                 `json:"github_ci_ok"`
	LocalCIOK   *bool                `json:"local_ci_ok"`
	BlockingPRs []statusJSONBlocking `json:"blocking_prs"`
}

type statusJSONResult struct {
	Stacks          []statusJSONStack `json:"stacks"`
	ShippableCount  int               `json:"shippable_count"`
	PendingCount    int               `json:"pending_count"`
	BlockedCount    int               `json:"blocked_count"`
	IncompleteCount int               `json:"incomplete_count"`
}

// PrintMergeStatusJSON outputs the shippability analysis as JSON.
func PrintMergeStatusJSON(out output.Output, result *shippable.AnalysisResult) error {
	stacks := make([]statusJSONStack, len(result.Stacks))
	for i, s := range result.Stacks {
		blocking := make([]statusJSONBlocking, len(s.BlockingPRs))
		for j, b := range s.BlockingPRs {
			blocking[j] = statusJSONBlocking{
				Branch:   b.Branch,
				PRNumber: b.PRNumber,
				Reason:   b.Reason,
			}
		}
		stacks[i] = statusJSONStack{
			RootBranch:  s.Stack.RootBranch,
			AllBranches: s.Stack.AllBranches,
			PRCount:     s.Stack.PRCount,
			Scope:       s.Stack.Scope,
			Status:      s.Status,
			Author:      s.Author,
			PRTitle:     s.PRTitle,
			ApprovalOK:  s.ApprovalOK,
			GitHubCIOK:  s.GitHubCIOK,
			LocalCIOK:   s.LocalCIOK,
			BlockingPRs: blocking,
		}
	}

	view := statusJSONResult{
		Stacks:          stacks,
		ShippableCount:  result.ShippableCount,
		PendingCount:    result.PendingCount,
		BlockedCount:    result.BlockedCount,
		IncompleteCount: result.IncompleteCount,
	}

	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	out.Print(string(data) + "\n")
	return nil
}
