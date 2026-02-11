package absorb

import "github.com/getstackit/stackit/internal/git"

// UnabsorbableReason describes why a hunk could not be absorbed.
type UnabsorbableReason string

const (
	ReasonCommutesWithAll UnabsorbableReason = "commutes_with_all"
	ReasonUnknownBranch   UnabsorbableReason = "unknown_branch"
	ReasonBinary          UnabsorbableReason = "binary"
	ReasonNewFile         UnabsorbableReason = "new_file"
	ReasonDeletedFile     UnabsorbableReason = "deleted_file"
	ReasonUnsupported     UnabsorbableReason = "unsupported"
)

// Unabsorbable represents a hunk that could not be absorbed and why.
type Unabsorbable struct {
	Hunk   git.Hunk
	Reason UnabsorbableReason
}

// Description returns a user-friendly explanation for why a hunk could not be absorbed.
func (r UnabsorbableReason) Description() string {
	switch r {
	case ReasonCommutesWithAll:
		return "No unique target commit found"
	case ReasonUnknownBranch:
		return "Target commit is not in a tracked branch"
	case ReasonBinary:
		return "Binary files cannot be absorbed"
	case ReasonNewFile:
		return "New files cannot be absorbed"
	case ReasonDeletedFile:
		return "Deleted files cannot be absorbed"
	case ReasonUnsupported:
		return "Unsupported change type"
	default:
		return string(r)
	}
}

// Tip returns a short actionable hint for this unabsorbable reason.
func (r UnabsorbableReason) Tip() string {
	switch r {
	case ReasonCommutesWithAll:
		return "Use --patch to stage smaller hunks with clearer ownership, then rerun absorb."
	case ReasonNewFile:
		return "Commit new files with create/modify, then rerun absorb."
	case ReasonDeletedFile:
		return "Commit file deletions with modify, then rerun absorb."
	case ReasonBinary:
		return "Commit binary changes with modify, then rerun absorb."
	case ReasonUnknownBranch:
		return "Run stackit restack (or sync) to refresh branch metadata, then rerun absorb."
	default:
		return ""
	}
}
