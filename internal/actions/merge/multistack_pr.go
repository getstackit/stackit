package merge

import (
	"context"
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
)

// MultiStackPRCreator handles creating the multi-stack PR
type MultiStackPRCreator struct {
	ctx          *app.Context
	worktreeEng  engine.Engine
	worktreePath string
	prGenerator  *PRContentGenerator
}

// NewMultiStackPRCreator creates a new PR creator for multi-stack merge
func NewMultiStackPRCreator(ctx *app.Context, worktreeEng engine.Engine, worktreePath string) *MultiStackPRCreator {
	return &MultiStackPRCreator{
		ctx:          ctx,
		worktreeEng:  worktreeEng,
		worktreePath: worktreePath,
		prGenerator:  NewPRContentGenerator(worktreeEng),
	}
}

// GenerateMultiStackBranchName creates a unique branch name for the multi-stack PR
func GenerateMultiStackBranchName() string {
	return fmt.Sprintf("multi-stack/%s", time.Now().Format("20060102-150405"))
}

// CreateAndPushBranch creates a named branch at the current HEAD and pushes it
func (p *MultiStackPRCreator) CreateAndPushBranch(ctx context.Context, branchName string) error {
	// Create a branch at current HEAD
	if err := p.worktreeEng.CreateBranch(ctx, branchName, "HEAD"); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	// Checkout the new branch
	branch := p.worktreeEng.GetBranch(branchName)
	if err := p.worktreeEng.CheckoutBranch(ctx, branch); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// Push to remote
	remote := p.worktreeEng.GetRemote()
	if err := p.worktreeEng.PushBranch(ctx, branch, remote, git.PushOptions{
		Force:    false,
		NoVerify: true,
	}); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}

// CreatePR creates the multi-stack pull request
func (p *MultiStackPRCreator) CreatePR(ctx context.Context, branchName string, included []MultiStackInfo, excluded []MultiStackExcluded) (*github.PullRequestInfo, error) {
	if _, err := p.ctx.RequireGitHub(); err != nil {
		return nil, err
	}

	content := p.prGenerator.GenerateMultiStackPR(included, excluded)

	opts := github.CreatePROptions{
		Title: content.Title,
		Body:  content.Body,
		Head:  branchName,
		Base:  p.worktreeEng.Trunk().GetName(),
		Draft: false,
	}

	pr, err := p.ctx.GitHub().CreatePullRequest(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return pr, nil
}

// WaitAndMerge waits for CI to pass and auto-merges the PR
func (p *MultiStackPRCreator) WaitAndMerge(ctx context.Context, branchName string, pr *github.PullRequestInfo, commitBody string) error {
	if _, err := p.ctx.RequireGitHub(); err != nil {
		return err
	}

	mergeMethod, err := p.resolveMergeMethod()
	if err != nil {
		return fmt.Errorf("failed to get merge method: %w", err)
	}

	waiter := NewCIWaiter(CIWaiterOptions{
		Client: p.ctx.GitHub(),
		Output: p.ctx.Output,
	})

	return waiter.WaitAndMerge(ctx, branchName, pr, true, github.MergePROptions{
		Method:     mergeMethod,
		CommitBody: commitBody,
	})
}

// EnableAutoMerge enables GitHub auto-merge for a multi-stack PR.
func (p *MultiStackPRCreator) EnableAutoMerge(ctx context.Context, pr *github.PullRequestInfo, commitBody string) error {
	if _, err := p.ctx.RequireGitHub(); err != nil {
		return err
	}
	if pr == nil || pr.NodeID == "" {
		return fmt.Errorf("missing pull request node id")
	}

	mergeMethod, err := p.resolveMergeMethod()
	if err != nil {
		return fmt.Errorf("failed to get merge method: %w", err)
	}

	return github.EnableAutoMerge(ctx, p.ctx.Git(), pr.NodeID, github.EnableAutoMergeOptions{ //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
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

func (p *MultiStackPRCreator) resolveMergeMethod() (github.MergeMethod, error) {
	cfg, err := config.LoadConfig(p.ctx.RepoRoot)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	mergeMethod := github.MergeMethodSquash // default
	if method := cfg.MergeMethod(); method != "" {
		switch method {
		case "merge":
			mergeMethod = github.MergeMethodMerge
		case "rebase":
			mergeMethod = github.MergeMethodRebase
		}
	}

	return mergeMethod, nil
}
