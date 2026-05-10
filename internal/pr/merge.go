package pr

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// FormatMergeTitleWithDescription formats a merge PR title, using the stack description if present.
// If the description has a title, it is used directly. Otherwise, falls back to FormatMergeTitle.
func FormatMergeTitleWithDescription(desc *git.StackDescription, scopes []string, totalCount int) string {
	if desc != nil && desc.Title != "" {
		return desc.Title
	}
	return FormatMergeTitle(scopes, totalCount)
}

// FormatMergeTitle formats a unified title for merge PRs (both multi-stack and consolidation).
// scopes contains the scope values for each branch (may include empty strings for unscoped branches).
// totalCount is the total number of PRs/branches being merged.
//
// Examples:
//   - Merging PROJ-123, PROJ-124     (all scoped)
//   - Merging PROJ-123 (+2)          (1 scoped, 2 unscoped)
//   - Merging 3 PRs                  (no scopes)
func FormatMergeTitle(scopes []string, totalCount int) string {
	// Collect unique non-empty scopes while preserving order
	seen := make(map[string]bool)
	var uniqueScopes []string
	for _, s := range scopes {
		if s != "" && !seen[s] {
			seen[s] = true
			uniqueScopes = append(uniqueScopes, s)
		}
	}

	if len(uniqueScopes) == 0 {
		return fmt.Sprintf("Merging %d PRs", totalCount)
	}

	scopeList := strings.Join(uniqueScopes, ", ")
	unscopedCount := totalCount - len(uniqueScopes)

	if unscopedCount <= 0 {
		return fmt.Sprintf("Merging %s", scopeList)
	}

	return fmt.Sprintf("Merging %s (+%d)", scopeList, unscopedCount)
}

// MergeBodyParams contains all parameters for generating a merge PR body.
// This unified structure handles both single-stack and multi-stack merges.
type MergeBodyParams struct {
	// Branches contains all branches being merged, with their PR info.
	Branches []MergeBranch

	// Excluded contains branches/stacks that were excluded from the merge.
	// Optional - only populated for multi-stack merges with conflicts.
	Excluded []ExcludedBranch

	// StackTree is the ASCII tree representation of the stack structure.
	// Optional - if empty, no tree section is rendered.
	StackTree string

	// StackDescription is the description of the stack being merged.
	// Optional - if present, it appears at the top of the body.
	StackDescription *git.StackDescription

	// Metadata is stack metadata for trailers.
	// Optional - if StackSize is zero, metadata is derived from Branches.
	Metadata StackMetadata
}

// MergeBranch represents a branch being merged.
type MergeBranch struct {
	Name     string
	PRNumber int    // 0 if no PR exists
	PRTitle  string // Empty if no PR exists
}

// ExcludedBranch represents a branch or stack excluded from the merge.
type ExcludedBranch struct {
	Name   string
	Reason string
}

// FormatMergeBody formats the body for a merge PR.
// This unified format works for both consolidation and multi-stack merges.
func FormatMergeBody(params MergeBodyParams) string {
	var body strings.Builder

	// Stack description (if present)
	if params.StackDescription != nil && !params.StackDescription.IsEmpty() {
		descContent := formatStackDescription(params.StackDescription)
		if descContent != "" {
			body.WriteString(descContent)
			body.WriteString("\n\n")
		}
	}

	// Header
	body.WriteString("This PR merges the following changes:\n\n")

	// Branch list with PR info
	for i, branch := range params.Branches {
		if branch.PRNumber > 0 {
			fmt.Fprintf(&body, "%d. **#%d** %s\n", i+1, branch.PRNumber, branch.PRTitle)
		} else {
			fmt.Fprintf(&body, "%d. %s\n", i+1, branch.Name)
		}
	}

	// Excluded section (only if there are exclusions)
	if len(params.Excluded) > 0 {
		body.WriteString("\n### Excluded\n\n")
		for _, ex := range params.Excluded {
			fmt.Fprintf(&body, "- **%s** — %s\n", ex.Name, ex.Reason)
		}
	}

	// Stack tree (only if provided)
	if params.StackTree != "" {
		body.WriteString("\n### Stack\n\n")
		body.WriteString("```\n")
		body.WriteString(params.StackTree)
		body.WriteString("```\n")
	}

	// Append stack trailers as a fallback for repos whose squash merge commit
	// message setting uses "PR body". Trailers survive automatically in that case.
	if len(params.Branches) > 0 {
		metadata := params.Metadata
		if metadata.StackSize == 0 {
			metadata = BuildStackMetadata(params.Branches, metadata.Scope)
		}
		body.WriteString("\n")
		body.WriteString(metadata.ToTrailers())
	}

	return body.String()
}

// StackTreeParams contains parameters for generating a stack tree visualization.
type StackTreeParams struct {
	TrunkName string
	Branches  []StackTreeBranch
}

// StackTreeBranch represents a branch in the stack tree.
type StackTreeBranch struct {
	Name     string
	Depth    int // 0 for branches directly off trunk
	PRNumber int // 0 if no PR
}

// FormatStackTree creates an ASCII tree representation of the stack.
func FormatStackTree(params StackTreeParams) string {
	var tree strings.Builder

	tree.WriteString(params.TrunkName + "\n")

	for _, branch := range params.Branches {
		indent := strings.Repeat("  ", branch.Depth+1)
		prSuffix := ""
		if branch.PRNumber > 0 {
			prSuffix = fmt.Sprintf(" (#%d)", branch.PRNumber)
		}
		fmt.Fprintf(&tree, "%s└─ %s%s\n", indent, branch.Name, prSuffix)
	}

	return tree.String()
}
