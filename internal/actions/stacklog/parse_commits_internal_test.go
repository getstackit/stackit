package stacklog

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"

	"github.com/stretchr/testify/require"
)

// TestMapCommits retains empty subjects without shifting their identities.
func TestMapCommits(t *testing.T) {
	t.Parallel()

	records := git.Commits{
		{SHA: "aaaa", Subject: "feat: first"},
		{SHA: "bbbb"}, // empty subject: must still produce a Commit
		{SHA: "cccc", Subject: "fix: third"},
	}

	got := mapCommits(records)
	require.Equal(t, []Commit{
		{SHA: "aaaa", Subject: "feat: first"},
		{SHA: "bbbb", Subject: ""},
		{SHA: "cccc", Subject: "fix: third"},
	}, got)
}
