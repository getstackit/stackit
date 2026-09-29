package merge

import (
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// ProgressEvent is a progress notification emitted while merging individual
// PRs (MergePR) or draining a stack (Drain). Adapters render these; the
// action never writes merge progress to the terminal itself.
type ProgressEvent interface {
	isProgressEvent()
}

// ProgressHandler receives progress events from MergePR and Drain.
type ProgressHandler interface {
	OnProgress(event ProgressEvent)
}

// NullProgressHandler discards all progress events.
type NullProgressHandler struct{}

// OnProgress implements ProgressHandler.
func (NullProgressHandler) OnProgress(ProgressEvent) {}

// PRAlreadyMergedEvent reports that the PR was already merged before any
// merge attempt was made.
type PRAlreadyMergedEvent struct {
	PRNumber git.PRNumber
}

// PRMergingDirectlyEvent reports that the PR is being merged directly through
// the REST API. AfterWaiting is set when the PR only became mergeable after
// polling (auto-merge unavailable); MergeState is the state it reached.
type PRMergingDirectlyEvent struct {
	PRNumber     git.PRNumber
	Method       github.MergeMethod
	MergeState   string
	AfterWaiting bool
}

// PRMergedEvent reports that the PR merged. External is set when it was
// merged by someone else while stackit was waiting.
type PRMergedEvent struct {
	PRNumber git.PRNumber
	External bool
}

// PREnablingAutomergeEvent reports that auto-merge is being enabled.
type PREnablingAutomergeEvent struct {
	PRNumber git.PRNumber
	Method   github.MergeMethod
}

// PRAutomergeEnabledEvent reports that auto-merge was enabled.
type PRAutomergeEnabledEvent struct {
	PRNumber git.PRNumber
}

// PRWaitingForMergeEvent reports that stackit is waiting for GitHub to merge
// the PR after enabling auto-merge.
type PRWaitingForMergeEvent struct {
	PRNumber git.PRNumber
}

// PRWaitingForMergeableEvent reports that auto-merge is not enabled on the
// repository, so stackit is polling until the PR becomes mergeable.
type PRWaitingForMergeableEvent struct {
	PRNumber git.PRNumber
}

// PRCleanStatusRaceEvent reports that enabling auto-merge failed because the
// PR became ready in the meantime, so it will be merged directly.
type PRCleanStatusRaceEvent struct {
	PRNumber git.PRNumber
}

// DrainPRStartedEvent reports that drain is about to merge the next PR.
// Position is 1-based; Total is the number of PRs this drain targets.
type DrainPRStartedEvent struct {
	BranchName string
	PRNumber   git.PRNumber
	Position   int
	Total      int
}

// DrainSyncingEvent reports that drain is syncing trunk and restacking the
// remaining branches after a merge.
type DrainSyncingEvent struct {
	PRNumber git.PRNumber
}

// DrainBranchPushedEvent reports the result of publishing one restacked
// branch after a merge. Failed is set (with Err) when the update failed.
type DrainBranchPushedEvent struct {
	BranchName string
	URL        string
	Failed     bool
	Err        error
}

// DrainStoppedEvent reports that drain stopped early because re-planning
// failed after at least one PR had merged (typically the stack is drained).
type DrainStoppedEvent struct {
	Merged int
	Reason error
}

func (PRAlreadyMergedEvent) isProgressEvent()       {}
func (PRMergingDirectlyEvent) isProgressEvent()     {}
func (PRMergedEvent) isProgressEvent()              {}
func (PREnablingAutomergeEvent) isProgressEvent()   {}
func (PRAutomergeEnabledEvent) isProgressEvent()    {}
func (PRWaitingForMergeEvent) isProgressEvent()     {}
func (PRWaitingForMergeableEvent) isProgressEvent() {}
func (PRCleanStatusRaceEvent) isProgressEvent()     {}
func (DrainPRStartedEvent) isProgressEvent()        {}
func (DrainSyncingEvent) isProgressEvent()          {}
func (DrainBranchPushedEvent) isProgressEvent()     {}
func (DrainStoppedEvent) isProgressEvent()          {}
