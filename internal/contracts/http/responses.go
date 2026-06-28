// Package httpcontract defines JSON-serializable API response types for stackit HTTP surfaces.
//
// These types form the API contract between the Go server and the frontend.
// They are intentionally decoupled from engine internals to allow the API
// to evolve independently.
package httpcontract

// ConfigResponse advertises server capabilities so the web client can adapt
// at runtime instead of relying on build-time flags. It is served
// unauthenticated so the client can learn whether a login is required
// before attempting one.
type ConfigResponse struct {
	// ReadOnly is true when the server refuses writes (the submit endpoint
	// is disabled). The client hides write affordances.
	ReadOnly bool `json:"readOnly"`
	// AuthRequired is true when reads require a session. It is false on a
	// public read-only server and when auth is disabled, so the client knows
	// not to send the user through a login flow.
	AuthRequired bool `json:"authRequired"`
	// SingleRepo is true when the server runs in single-repo mode (the local
	// `-cwd`/discovery shortcut, not the DB-backed multi-tenant model). The
	// client skips the repo picker and opens the sole repo directly — picking
	// a repo is only meaningful in the hosted, multi-repo model.
	SingleRepo bool `json:"singleRepo"`
}

// RepoSummary is one entry in ReposListResponse — the metadata clients
// need to render a repo picker without hitting per-repo endpoints. Owner and
// Repo are the GitHub coordinates the client uses to build /{owner}/{repo}
// routes; they are omitted for a repo with no GitHub remote.
type RepoSummary struct {
	ID            string `json:"id"`
	Owner         string `json:"owner,omitempty"`
	Repo          string `json:"repo,omitempty"`
	DisplayName   string `json:"displayName"`
	Trunk         string `json:"trunk"`
	CurrentBranch string `json:"currentBranch,omitempty"`
}

// ReposListResponse is the response shape for GET /api/v1/repos.
type ReposListResponse struct {
	Repos []RepoSummary `json:"repos"`
}

// OnboardRepoRequest is the body for POST /api/v1/repos — a request to clone a
// GitHub repository and start serving it. Provide either Owner+Name or URL;
// URL is a convenience that is parsed into owner/name. On success the server
// responds 201 with a RepoSummary for the newly served repo.
type OnboardRepoRequest struct {
	Owner string `json:"owner,omitempty"`
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ViewResponse is the combined response for the frontend view.
// It bundles repo metadata and all stack details into a single payload
// to avoid N+1 API calls.
type ViewResponse struct {
	Repo           RepoResponse          `json:"repo"`
	Stacks         []StackDetail         `json:"stacks"`
	RecentlyMerged []TrunkCommitResponse `json:"recentlyMerged,omitempty"`
}

const (
	// TrunkCommitKindRegular is a normal non-stack merge trunk commit.
	TrunkCommitKindRegular = "regular"
	// TrunkCommitKindStackMerge is a stack consolidation merge commit.
	TrunkCommitKindStackMerge = "stack-merge"
)

// TrunkCommitResponse represents a commit on the trunk branch,
// optionally enriched with stack metadata from git trailers.
type TrunkCommitResponse struct {
	SHA           string         `json:"sha"`
	Message       string         `json:"message"`
	Author        string         `json:"author"`
	Date          string         `json:"date"`
	Kind          string         `json:"kind"`
	PRNumber      int            `json:"prNumber,omitempty"`
	StackSize     int            `json:"stackSize,omitempty"`
	StackPRs      []int          `json:"stackPRs,omitempty"`
	StackPRTitles map[int]string `json:"stackPRTitles,omitempty"`
	StackScope    string         `json:"stackScope,omitempty"`
}

// RepoResponse contains repository metadata.
type RepoResponse struct {
	Owner         string `json:"owner"`
	Repo          string `json:"repo"`
	Trunk         string `json:"trunk"`
	CurrentBranch string `json:"currentBranch"`
	Remote        string `json:"remote"`
	CurrentUser   string `json:"currentUser,omitempty"`
}

// StackSummary is a lightweight summary of a stack for list views.
type StackSummary struct {
	RootBranch  string `json:"rootBranch"`
	Title       string `json:"title"`
	Status      string `json:"status"` // shippable, pending, blocked, incomplete
	Scope       string `json:"scope,omitempty"`
	BranchCount int    `json:"branchCount"`
	PRCount     int    `json:"prCount"`
	IsCurrent   bool   `json:"isCurrent"`
	HasWorktree bool   `json:"hasWorktree,omitempty"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

// StackDetail is a full stack with all branch details.
type StackDetail struct {
	StackSummary
	Branches []BranchResponse `json:"branches"` // DFS order
}

// BranchResponse contains all information about a single branch.
type BranchResponse struct {
	Name         string           `json:"name"`
	Parent       string           `json:"parent,omitempty"`
	Children     []string         `json:"children,omitempty"`
	Depth        int              `json:"depth"`
	IsCurrent    bool             `json:"isCurrent"`
	NeedsRestack bool             `json:"needsRestack"`
	IsLocked     bool             `json:"isLocked"`
	LockReason   string           `json:"lockReason,omitempty"`
	IsFrozen     bool             `json:"isFrozen"`
	Scope        string           `json:"scope,omitempty"`
	Revision     string           `json:"revision"`
	CommitDate   string           `json:"commitDate"`
	CommitAuthor string           `json:"commitAuthor"`
	CommitCount  int              `json:"commitCount"`
	LinesAdded   int              `json:"linesAdded"`
	LinesDeleted int              `json:"linesDeleted"`
	Commits      []CommitResponse `json:"commits,omitempty"`
	PR           *PRResponse      `json:"pr,omitempty"`
	CI           *CIResponse      `json:"ci,omitempty"`
	RemoteStatus *RemoteStatus    `json:"remoteStatus,omitempty"`
}

// BranchDiffResponse contains raw patch data for a branch relative to its divergence point.
type BranchDiffResponse struct {
	Branch       string `json:"branch"`
	BaseRevision string `json:"baseRevision"`
	HeadRevision string `json:"headRevision"`
	Patch        string `json:"patch"`
}

// PRResponse contains pull request metadata.
type PRResponse struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"` // OPEN, MERGED, CLOSED
	URL     string `json:"url"`
	IsDraft bool   `json:"isDraft"`
	Base    string `json:"base"`
}

// CIResponse contains CI/review status for a branch.
type CIResponse struct {
	Status         string                `json:"status"` // passing, failing, pending, none
	ReviewDecision string                `json:"reviewDecision"`
	Checks         []CheckDetailResponse `json:"checks,omitempty"`
}

// CheckDetailResponse contains details about a single CI check.
type CheckDetailResponse struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// CommitResponse represents a single commit.
type CommitResponse struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Date    string `json:"date,omitempty"`
}

// RemoteStatus describes how a branch relates to its remote tracking branch.
type RemoteStatus struct {
	Ahead         bool `json:"ahead"`
	Behind        bool `json:"behind"`
	Diverged      bool `json:"diverged"`
	MissingRemote bool `json:"missingRemote"`
}
