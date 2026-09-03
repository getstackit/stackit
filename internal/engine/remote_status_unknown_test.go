package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

// A status whose merge base could not be resolved reports false from Behind,
// Ahead and Diverged alike — the same answers a fully in-sync branch gives.
// Unknown is what separates "nothing to do" from "could not tell", so callers
// that act on being up to date have something to branch on.
func TestBranchRemoteStatusUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  engine.BranchRemoteStatus
		unknown bool
	}{
		{
			name:    "unresolvable merge base is unknown",
			status:  engine.BranchRemoteStatus{LocalSha: "aaa", RemoteSha: "bbb", CommonAncestor: ""},
			unknown: true,
		},
		{
			name:    "matching shas are known and in sync",
			status:  engine.BranchRemoteStatus{LocalSha: "aaa", RemoteSha: "aaa", CommonAncestor: "aaa"},
			unknown: false,
		},
		{
			name:    "resolvable merge base is known",
			status:  engine.BranchRemoteStatus{LocalSha: "aaa", RemoteSha: "bbb", CommonAncestor: "aaa"},
			unknown: false,
		},
		{
			name:    "no remote ref is not unknown, it is absent",
			status:  engine.BranchRemoteStatus{LocalSha: "aaa", RemoteSha: "", CommonAncestor: ""},
			unknown: false,
		},
		{
			name:    "no local ref is not unknown, it is absent",
			status:  engine.BranchRemoteStatus{LocalSha: "", RemoteSha: "bbb", CommonAncestor: ""},
			unknown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.unknown, tt.status.Unknown())
		})
	}
}

// The ambiguity this guards against. An unresolvable status answers every other
// predicate with a definite-looking value derived from missing data: Behind and
// Ahead are false (so a caller checking only Behind concludes there is nothing
// to pull), while Diverged is true purely because an empty common ancestor
// equals neither sha. Unknown is the only honest signal.
func TestBranchRemoteStatusUnknownIsIndistinguishableWithoutUnknown(t *testing.T) {
	t.Parallel()

	status := engine.BranchRemoteStatus{LocalSha: "aaa", RemoteSha: "bbb", CommonAncestor: ""}

	require.True(t, status.Unknown())

	// Reads as "nothing to pull", which is what made sync --dry-run report a
	// clean preview while trunk was behind.
	require.False(t, status.Behind())
	require.False(t, status.Ahead())
	require.False(t, status.Matches())

	// And reads as a definite divergence, which is equally unfounded.
	require.True(t, status.Diverged())
}
