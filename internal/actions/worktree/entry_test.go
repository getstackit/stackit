package worktree

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryJSONProjectsTypedLifecycleForCompatibility(t *testing.T) {
	t.Parallel()

	entry := Entry{
		AnchorBranch: "anchor",
		Lifecycle: WorktreeLifecycle{
			Registration: RegistrationStateInvalid,
			Presence:     WorktreeMissing,
			Checkout:     WorktreeCheckoutElsewhere,
			Changes:      WorktreeChangesClean,
			Remove:       LifecycleAllowed,
			Detach:       LifecycleAllowed,
		},
	}

	data, err := json.Marshal(entry)
	require.NoError(t, err)

	var result struct {
		Exists            bool              `json:"exists"`
		RegistrationState RegistrationState `json:"registration_state"`
		CanRemove         bool              `json:"can_remove"`
		CanDetach         bool              `json:"can_detach"`
		NeedsRepair       bool              `json:"needs_repair"`
		Lifecycle         WorktreeLifecycle `json:"lifecycle"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.False(t, result.Exists)
	require.Equal(t, RegistrationStateInvalid, result.RegistrationState)
	require.True(t, result.CanRemove)
	require.True(t, result.CanDetach)
	require.True(t, result.NeedsRepair)
	require.Equal(t, entry.Lifecycle, result.Lifecycle)
}
