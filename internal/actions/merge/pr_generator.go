package merge

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/pr"
)

// PRContentGenerator handles PR title and body generation for merge PRs.
type PRContentGenerator struct {
	engine interface {
		GetBranch(name string) engine.Branch
		GetScope(branch engine.Branch) engine.Scope
		GetStackDescription(branch engine.Branch) *git.StackDescription
		Trunk() engine.Branch
	}
}

// NewPRContentGenerator creates a new PR content generator.
func NewPRContentGenerator(engine interface {
	GetBranch(name string) engine.Branch
	GetScope(branch engine.Branch) engine.Scope
	GetStackDescription(branch engine.Branch) *git.StackDescription
	Trunk() engine.Branch
}) *PRContentGenerator {
	return &PRContentGenerator{engine: engine}
}

// GenerateMultiStackPR generates PR content for multi-stack merges.
func (g *PRContentGenerator) GenerateMultiStackPR(included []MultiStackInfo, excluded []MultiStackExcluded) pr.Content {
	branches := g.collectMultiStackBranches(included)
	scopes := g.collectScopes(branches)

	title := pr.FormatMergeTitle(scopes, len(branches))
	body := g.generateBodyWithOptions(branches, g.convertExcluded(excluded), nil, pr.ResolveUnifiedScope(scopes))

	return pr.Content{Title: title, Body: body}
}

// GenerateConsolidationPR generates PR content for stack consolidation.
func (g *PRContentGenerator) GenerateConsolidationPR(branches []BranchMergeInfo) pr.Content {
	mergeBranches := g.convertBranchMergeInfo(branches)
	scopes := g.collectScopes(mergeBranches)

	// Get stack description from the root branch (first branch in the list)
	var stackDesc *git.StackDescription
	if len(branches) > 0 {
		rootBranch := g.engine.GetBranch(branches[0].BranchName)
		stackDesc = g.engine.GetStackDescription(rootBranch)
	}

	title := pr.FormatMergeTitleWithDescription(stackDesc, scopes, len(mergeBranches))
	body := g.generateBodyWithOptions(mergeBranches, nil, stackDesc, pr.ResolveUnifiedScope(scopes))

	return pr.Content{Title: title, Body: body}
}

// generateBodyWithOptions creates the PR body with branches, exclusions, stack tree, optional description, and scope.
func (g *PRContentGenerator) generateBodyWithOptions(branches []pr.MergeBranch, excluded []pr.ExcludedBranch, stackDesc *git.StackDescription, scope string) string {
	// Build stack tree
	treeBranches := make([]pr.StackTreeBranch, len(branches))
	for i, branch := range branches {
		b := g.engine.GetBranch(branch.Name)
		depth := g.calculateDepth(b)
		treeBranches[i] = pr.StackTreeBranch{
			Name:     branch.Name,
			Depth:    depth,
			PRNumber: branch.PRNumber,
		}
	}

	stackTree := pr.FormatStackTree(pr.StackTreeParams{
		TrunkName: g.engine.Trunk().GetName(),
		Branches:  treeBranches,
	})

	return pr.FormatMergeBody(pr.MergeBodyParams{
		Branches:         branches,
		Excluded:         excluded,
		StackTree:        stackTree,
		StackDescription: stackDesc,
		Metadata:         pr.BuildStackMetadata(branches, scope),
	})
}

// collectMultiStackBranches gathers all branches from included stacks.
func (g *PRContentGenerator) collectMultiStackBranches(included []MultiStackInfo) []pr.MergeBranch {
	var branches []pr.MergeBranch
	for _, stack := range included {
		for _, branchName := range stack.AllBranches {
			branch := g.engine.GetBranch(branchName)
			mb := pr.MergeBranch{Name: branchName}

			if prInfo, err := branch.GetPrInfo(); err == nil && prInfo != nil && prInfo.Number() != nil {
				mb.PRNumber = *prInfo.Number()
				mb.PRTitle = prInfo.Title()
			}

			branches = append(branches, mb)
		}
	}
	return branches
}

// convertBranchMergeInfo converts BranchMergeInfo to MergeBranch.
func (g *PRContentGenerator) convertBranchMergeInfo(branches []BranchMergeInfo) []pr.MergeBranch {
	result := make([]pr.MergeBranch, len(branches))
	for i, branchInfo := range branches {
		mb := pr.MergeBranch{
			Name:     branchInfo.BranchName,
			PRNumber: branchInfo.PRNumber,
		}

		// Get PR title from engine if we have a PR number
		if branchInfo.PRNumber > 0 {
			branch := g.engine.GetBranch(branchInfo.BranchName)
			if prInfo, err := branch.GetPrInfo(); err == nil && prInfo != nil {
				mb.PRTitle = prInfo.Title()
			}
		}

		result[i] = mb
	}
	return result
}

// convertExcluded converts MultiStackExcluded to ExcludedBranch.
func (g *PRContentGenerator) convertExcluded(excluded []MultiStackExcluded) []pr.ExcludedBranch {
	if len(excluded) == 0 {
		return nil
	}

	result := make([]pr.ExcludedBranch, len(excluded))
	for i, ex := range excluded {
		result[i] = pr.ExcludedBranch{
			Name:   ex.Stack.RootBranch,
			Reason: ex.Reason,
		}
	}
	return result
}

// collectScopes gathers scope values for all branches.
func (g *PRContentGenerator) collectScopes(branches []pr.MergeBranch) []string {
	scopes := make([]string, len(branches))
	for i, mb := range branches {
		branch := g.engine.GetBranch(mb.Name)
		scope := g.engine.GetScope(branch)
		scopes[i] = scope.String()
	}
	return scopes
}

// calculateDepth computes how deep a branch is in the stack.
func (g *PRContentGenerator) calculateDepth(branch engine.Branch) int {
	depth := 0
	parent := branch.GetParent()
	for parent != nil && !parent.IsTrunk() {
		depth++
		parent = parent.GetParent()
	}
	return depth
}
