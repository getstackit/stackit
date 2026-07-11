// Package github provides a client for interacting with the GitHub API.
package github

import (
	"github.com/getstackit/stackit/internal/git"

	"context"
	"strings"
	"time"

	"github.com/google/go-github/v88/github"
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
	State   git.PRState
	Draft   bool
	Base    string
	Head    string
	// Warnings contains non-fatal issues that occurred during PR creation/update
	// (e.g., failed to add labels or assignees)
	Warnings []string
}

// CheckRunStatus is a GitHub check-run status.
type CheckRunStatus string

const (
	CheckRunStatusQueued     CheckRunStatus = "QUEUED"
	CheckRunStatusInProgress CheckRunStatus = "IN_PROGRESS"
	CheckRunStatusCompleted  CheckRunStatus = "COMPLETED"
)

// CheckConclusion is a GitHub check-run conclusion.
type CheckConclusion string

const (
	CheckConclusionSuccess        CheckConclusion = "SUCCESS"
	CheckConclusionFailure        CheckConclusion = "FAILURE"
	CheckConclusionNeutral        CheckConclusion = "NEUTRAL"
	CheckConclusionSkipped        CheckConclusion = "SKIPPED"
	CheckConclusionCanceled       CheckConclusion = "CANCELED"
	CheckConclusionTimedOut       CheckConclusion = "TIMED_OUT"
	CheckConclusionActionRequired CheckConclusion = "ACTION_REQUIRED"
)

// CheckDetail represents the status of an individual CI check
type CheckDetail struct {
	Name       string
	Status     CheckRunStatus
	Conclusion CheckConclusion
	StartedAt  time.Time
	FinishedAt time.Time
}

// CheckStatus represents the combined status of all CI checks for a PR
type CheckStatus struct {
	Passing        bool
	Pending        bool
	Checks         []CheckDetail
	ReviewDecision ReviewDecision // empty if none
	Author         string         // GitHub username of PR author
	State          git.PRState    // empty if unknown
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

// ValidMergeMethods lists every accepted merge method value.
var ValidMergeMethods = []MergeMethod{MergeMethodSquash, MergeMethodMerge, MergeMethodRebase}

// Valid reports whether m is one of the accepted merge methods.
func (m MergeMethod) Valid() bool {
	return m == MergeMethodSquash || m == MergeMethodMerge || m == MergeMethodRebase
}

// MergeMethodSettings contains the allowed merge methods for a repository
type MergeMethodSettings struct {
	AllowMergeCommit bool
	AllowSquashMerge bool
	AllowRebaseMerge bool
}

// MergePROptions configures how a pull request should be merged.
type MergePROptions struct {
	Method     MergeMethod
	CommitBody string // Optional commit body for merge/squash commit message.
}

// Client is an interface for GitHub API interactions
type Client interface {
	// CreatePullRequest creates a new pull request
	CreatePullRequest(ctx context.Context, opts CreatePROptions) (*PullRequestInfo, error)

	// UpdatePullRequest updates an existing pull request
	// Returns warnings (non-fatal issues like failed label/assignee additions) and error
	UpdatePullRequest(ctx context.Context, prNumber int, opts UpdatePROptions) (warnings []string, err error)

	// GetPullRequestByBranch gets a pull request for a branch
	GetPullRequestByBranch(ctx context.Context, branchName string) (*PullRequestInfo, error)

	// GetPullRequest gets a pull request by number
	GetPullRequest(ctx context.Context, prNumber int) (*PullRequestInfo, error)

	// MergePullRequest merges a pull request using the specified merge method
	MergePullRequest(ctx context.Context, branchName string, opts MergePROptions) error

	// GetAllowedMergeMethods returns the allowed merge methods for the repository
	GetAllowedMergeMethods(ctx context.Context) (*MergeMethodSettings, error)

	// GetPRChecksStatus returns the check status for a single branch
	GetPRChecksStatus(ctx context.Context, branchName string) (*CheckStatus, error)

	// BatchGetPRChecksStatus returns the check status for multiple branches
	BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (ChecksByBranch, error)

	// BatchGetPRTitles returns titles for multiple PRs by number
	BatchGetPRTitles(ctx context.Context, prNumbers []int) (map[int]string, error)

	// GetOwnerRepo returns the repository owner and name
	GetOwnerRepo() (owner, repo string)

	// ClosePullRequest closes a pull request
	ClosePullRequest(ctx context.Context, prNumber int) error

	// Comment methods for navigation location support
	// CreatePRComment creates a new comment on a pull request
	CreatePRComment(ctx context.Context, prNumber int, body string) (int64, error)

	// UpdatePRComment updates an existing pull request comment
	UpdatePRComment(ctx context.Context, commentID int64, body string) error

	// DeletePRComment deletes a pull request comment
	DeletePRComment(ctx context.Context, commentID int64) error

	// ListPRComments lists all comments on a pull request
	ListPRComments(ctx context.Context, prNumber int) ([]PRComment, error)

	// GetCurrentUser returns the authenticated GitHub username
	GetCurrentUser(ctx context.Context) (string, error)
}

// PRComment represents a comment on a pull request
type PRComment struct {
	ID   int64
	Body string
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
		info.State = git.PRState(strings.ToUpper(*pr.State))
	}
	// The REST API reports merged PRs as state "closed" with a separate merged
	// indicator; normalize to MERGED so this path matches the GraphQL
	// PullRequestState enum. List responses omit the `merged` boolean and only
	// populate `merged_at`, so check both.
	if pr.GetMerged() || pr.MergedAt != nil {
		info.State = git.PRStateMerged
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
