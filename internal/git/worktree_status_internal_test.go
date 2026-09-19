package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseWorktreeStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		want  WorktreeStatus
	}{
		{"", WorktreeStatus{}},
		{"?? file\nname\x00", WorktreeStatus{Untracked: true}},
		{"!! ignored\x00", WorktreeStatus{}},
		{"RM target\x00?? source\nname\x00", WorktreeStatus{Staged: true, Unstaged: true}},
		{"UU conflict\x00", WorktreeStatus{Staged: true, Unstaged: true}},
		{" D deleted\x00", WorktreeStatus{Unstaged: true}},
	} {
		got, err := parseWorktreeStatus(tc.input)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	for _, invalid := range []string{"bad", "x\x00", "R  target\x00", "??xfile\x00"} {
		_, err := parseWorktreeStatus(invalid)
		require.Error(t, err)
	}
}
