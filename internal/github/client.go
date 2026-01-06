// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
)

const (
	// MaxGitHubConcurrency limits the number of concurrent requests to GitHub
	// to avoid triggering secondary rate limits.
	MaxGitHubConcurrency = 10
)

// PullRequestInfo contains information about a pull request
// This is a simplified struct to avoid coupling to go-github library
type PullRequestInfo struct {
	Number  int
	NodeID  string
	HTMLURL string
	Title   string
	Body    string
	State   string
	Draft   bool
	Base    string
	Head    string
}

// CheckDetail represents the status of an individual CI check
type CheckDetail struct {
	Name       string
	Status     string // QUEUED, IN_PROGRESS, COMPLETED
	Conclusion string // SUCCESS, FAILURE, NEUTRAL, etc.
	StartedAt  time.Time
	FinishedAt time.Time
}

// CheckStatus represents the combined status of all CI checks for a PR
type CheckStatus struct {
	Passing bool
	Pending bool
	Checks  []CheckDetail
}

// MergeMethod represents a GitHub PR merge method
type MergeMethod string

const (
	// MergeMethodMerge creates a merge commit
	MergeMethodMerge MergeMethod = "merge"
	// MergeMethodSquash squashes commits before merging
	MergeMethodSquash MergeMethod = "squash"
	// MergeMethodRebase rebases commits onto base branch
	MergeMethodRebase MergeMethod = "rebase"
)

// MergeMethodSettings contains the allowed merge methods for a repository
type MergeMethodSettings struct {
	AllowMergeCommit bool
	AllowSquashMerge bool
	AllowRebaseMerge bool
}

// Client is an interface for GitHub API interactions
type Client interface {
	// CreatePullRequest creates a new pull request
	CreatePullRequest(ctx context.Context, owner, repo string, opts CreatePROptions) (*PullRequestInfo, error)

	// UpdatePullRequest updates an existing pull request
	UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, opts UpdatePROptions) error

	// GetPullRequestByBranch gets a pull request for a branch
	GetPullRequestByBranch(ctx context.Context, owner, repo, branchName string) (*PullRequestInfo, error)

	// GetPullRequest gets a pull request by number
	GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequestInfo, error)

	// MergePullRequest merges a pull request using the specified merge method
	MergePullRequest(ctx context.Context, branchName string, method MergeMethod) error

	// GetAllowedMergeMethods returns the allowed merge methods for the repository
	GetAllowedMergeMethods(ctx context.Context) (*MergeMethodSettings, error)

	// GetPRChecksStatus returns the check status for a single branch
	GetPRChecksStatus(ctx context.Context, branchName string) (*CheckStatus, error)

	// BatchGetPRChecksStatus returns the check status for multiple branches
	BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (map[string]*CheckStatus, error)

	// GetOwnerRepo returns the repository owner and name
	GetOwnerRepo() (owner, repo string)
}

// ToPullRequestInfo converts a github.PullRequest to PullRequestInfo
func ToPullRequestInfo(pr *github.PullRequest) *PullRequestInfo {
	if pr == nil {
		return nil
	}

	info := &PullRequestInfo{}

	if pr.Number != nil {
		info.Number = *pr.Number
	}
	if pr.NodeID != nil {
		info.NodeID = *pr.NodeID
	}
	if pr.HTMLURL != nil {
		info.HTMLURL = *pr.HTMLURL
	}
	if pr.Title != nil {
		info.Title = *pr.Title
	}
	if pr.Body != nil {
		info.Body = *pr.Body
	}
	if pr.State != nil {
		info.State = strings.ToUpper(*pr.State)
	}
	if pr.Draft != nil {
		info.Draft = *pr.Draft
	}
	if pr.Base != nil && pr.Base.Ref != nil {
		info.Base = *pr.Base.Ref
	}
	if pr.Head != nil && pr.Head.Ref != nil {
		info.Head = *pr.Head.Ref
	}

	return info
}
