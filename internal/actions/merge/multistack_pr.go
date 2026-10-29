package merge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
)

// MultiStackPRCreator handles creating the multi-stack PR
type MultiStackPRCreator struct {
	deps         Deps
	worktreeEng  engine.Engine
	worktreePath engine.WorktreePath
	prGenerator  *PRContentGenerator
}

// NewMultiStackPRCreator creates a new PR creator for multi-stack merge
func NewMultiStackPRCreator(deps Deps, worktreeEng engine.Engine, worktreePath engine.WorktreePath) *MultiStackPRCreator {
	return &MultiStackPRCreator{
		deps:         deps,
		worktreeEng:  worktreeEng,
		worktreePath: worktreePath,
		prGenerator:  NewPRContentGenerator(worktreeEng),
	}
}

// GenerateMultiStackBranchName creates a unique branch name for the multi-stack PR
func GenerateMultiStackBranchName() string {
	return fmt.Sprintf("multi-stack/%s-%d", time.Now().Format("20060102-150405"), time.Now().UnixNano())
}

// CreateAndPushBranch creates a named branch at the current HEAD and pushes it
func (p *MultiStackPRCreator) CreateAndPushBranch(ctx context.Context, branchName string) error {
	// Create a branch at current HEAD
	if err := p.worktreeEng.CreateBranch(ctx, branchName, "HEAD"); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}
	// Utility metadata tells sync that this generated delivery branch can be
	// safely cleaned up once the consolidation has landed.
	if err := p.worktreeEng.SetBranchType(ctx, p.worktreeEng.GetBranch(branchName), git.BranchTypeUtility); err != nil {
		return fmt.Errorf("mark multi-stack branch %s as utility: %w", branchName, err)
	}

	// Checkout the new branch
	branch := p.worktreeEng.GetBranch(branchName)
	if err := p.worktreeEng.CheckoutBranch(ctx, branch); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// Push to remote
	remote := p.worktreeEng.GetRemote(ctx)
	if err := p.worktreeEng.PushBranches(ctx, remote, []git.PushSpec{{BranchName: branch.Name(), LeaseMode: git.PushLeaseNone}}, git.PushOptions{
		Force:    false,
		NoVerify: true,
	}).One(); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}

// CreatePR creates the multi-stack pull request
func (p *MultiStackPRCreator) CreatePR(ctx context.Context, branchName string, included []MultiStackInfo, excluded []MultiStackExcluded) (*github.PullRequestInfo, error) {
	if err := p.deps.requireGitHub(); err != nil {
		return nil, err
	}

	content := p.prGenerator.GenerateMultiStackPR(included, excluded)

	opts := github.CreatePROptions{
		Title: content.Title,
		Body:  content.Body,
		Head:  branchName,
		Base:  p.worktreeEng.Trunk().Name(),
		Draft: false,
	}

	pr, err := p.deps.GitHub().CreatePullRequest(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return pr, nil
}

// WaitAndMerge waits for CI to pass and auto-merges the PR
func (p *MultiStackPRCreator) WaitAndMerge(ctx context.Context, branchName string, pr *github.PullRequestInfo, commitBody string) error {
	if err := p.deps.requireGitHub(); err != nil {
		return err
	}

	mergeMethod := p.resolveMergeMethod()

	waiter := NewCIWaiter(CIWaiterOptions{
		Client: p.deps.GitHub(),
		Output: p.deps.Out,
	})

	return waiter.WaitAndMerge(ctx, branchName, pr, true, github.MergePROptions{
		Method:     mergeMethod,
		CommitBody: commitBody,
	})
}

// EnableAutoMerge enables GitHub auto-merge for a multi-stack PR.
func (p *MultiStackPRCreator) EnableAutoMerge(ctx context.Context, pr *github.PullRequestInfo, commitBody string) error {
	if err := p.deps.requireGitHub(); err != nil {
		return err
	}
	if pr == nil || pr.NodeID == "" {
		return errors.New("missing pull request node id")
	}

	mergeMethod := p.resolveMergeMethod()

	return github.EnableAutoMerge(ctx, p.deps.GHRunner, pr.NodeID, github.EnableAutoMergeOptions{
		MergeMethod: mergeMethod,
		CommitBody:  commitBody,
	})
}

// BuildStackMetadata builds stack trailer metadata for the included multi-stack branches.
func (p *MultiStackPRCreator) BuildStackMetadata(included []MultiStackInfo) pr.StackMetadata {
	branches := p.prGenerator.collectMultiStackBranches(included)
	scopes := p.prGenerator.collectScopes(branches)
	return pr.BuildStackMetadata(branches, pr.ResolveUnifiedScope(scopes))
}

// resolveMergeMethod returns the configured merge method from the already-loaded
// repository config, defaulting to squash when unset or unavailable.
func (p *MultiStackPRCreator) resolveMergeMethod() github.MergeMethod {
	if p.deps.Config != nil {
		if method := p.deps.Config.MergeMethod(); method.Valid() {
			return method
		}
	}
	return github.MergeMethodSquash
}
