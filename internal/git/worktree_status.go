package git

import (
	"context"
	"fmt"
	"strings"
)

// WorktreeStatus is a point-in-time snapshot. Read it again after staging,
// committing, stashing, or any other index/worktree mutation.
type WorktreeStatus struct {
	Staged    bool
	Unstaged  bool
	Untracked bool
}

// Clean reports whether the index and worktree have no pending changes.
func (s WorktreeStatus) Clean() bool { return !s.Staged && !s.HasUnstagedChanges() }

// HasUnstagedChanges includes untracked files as well as tracked modifications.
func (s WorktreeStatus) HasUnstagedChanges() bool { return s.Unstaged || s.Untracked }

// ReadWorktreeStatus reads all three flags in one process. Explicit untracked
// reporting keeps guards independent of status.showUntrackedFiles configuration.
func (r *runner) ReadWorktreeStatus(ctx context.Context) (WorktreeStatus, error) {
	out, err := r.RunGitCommandRawWithContext(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return WorktreeStatus{}, err
	}
	return parseWorktreeStatus(out)
}

func parseWorktreeStatus(out string) (WorktreeStatus, error) {
	var status WorktreeStatus
	for out != "" {
		record, rest, ok := strings.Cut(out, "\x00")
		if !ok || len(record) < 4 || record[2] != ' ' {
			return WorktreeStatus{}, fmt.Errorf("malformed worktree status record")
		}
		out = rest
		x, y := record[0], record[1]
		switch {
		case x == '?' && y == '?':
			status.Untracked = true
		case x == '!' && y == '!':
			continue
		default:
			status.Staged = status.Staged || x != ' '
			status.Unstaged = status.Unstaged || y != ' '
		}
		// Renames/copies have a second NUL-delimited path, not another XY record.
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			_, out, ok = strings.Cut(out, "\x00")
			if !ok {
				return WorktreeStatus{}, fmt.Errorf("missing rename source in worktree status")
			}
		}
	}
	return status, nil
}
