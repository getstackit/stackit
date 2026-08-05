package worktree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPruneRefusal pins the decision that both `worktree prune` and
// `worktree prune --dry-run` read.
//
// The two modes used to decide separately, so --dry-run listed worktrees the
// real run then refused: a registration whose directory was gone but whose
// stack still had branches was reported prunable and then rejected by
// RemoveAction's root-branches guard, and an invalid registration that
// RemoveAction can clean up was sent to `worktree repair`, which cannot fix it.
func TestPruneRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entry         Entry
		currentAnchor string
		refused       bool
	}{
		{
			name:  "empty healthy worktree is prunable",
			entry: Entry{AnchorBranch: "anchor", Exists: true},
		},
		{
			name:  "missing directory is prunable",
			entry: Entry{AnchorBranch: "anchor"},
		},
		{
			name: "missing directory with stacked branches is refused",
			// Reported prunable by --dry-run, then refused by the real run.
			entry:   Entry{AnchorBranch: "anchor", RootBranches: []string{"feature"}, StackSize: 1},
			refused: true,
		},
		{
			name: "removable invalid registration is prunable",
			// repairHint sends users to a command that cannot fix this state,
			// but RemoveAction handles it.
			entry: Entry{
				AnchorBranch:      "anchor",
				NeedsRepair:       true,
				RegistrationState: RegistrationStateInvalid,
				CanRemove:         true,
			},
		},
		{
			name: "unrepairable registration is refused",
			entry: Entry{
				AnchorBranch:      "anchor",
				Exists:            true,
				NeedsRepair:       true,
				RegistrationState: RegistrationStateLegacy,
				StatusMessage:     "registration is legacy",
			},
			refused: true,
		},
		{
			name:          "current worktree is refused",
			entry:         Entry{AnchorBranch: "anchor", Exists: true},
			currentAnchor: "anchor",
			refused:       true,
		},
		{
			name:    "dirty worktree is refused",
			entry:   Entry{AnchorBranch: "anchor", Exists: true, IsDirty: true},
			refused: true,
		},
		{
			name: "dirty flag on a missing directory does not refuse",
			// Nothing on disk to protect: the flag is stale.
			entry: Entry{AnchorBranch: "anchor", IsDirty: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reason := pruneRefusal(&tt.entry, tt.currentAnchor)
			if tt.refused {
				require.NotEmpty(t, reason, "expected a refusal reason")
				return
			}
			require.Empty(t, reason)
		})
	}
}
