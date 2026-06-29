package stacklog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseCommits proves the combined SHA+subject record is split correctly and
// that a record with an empty subject still yields a Commit (it is not dropped) —
// the failure mode of the previous index-paired two-list approach.
func TestParseCommits(t *testing.T) {
	t.Parallel()

	records := []string{
		"aaaa\x00feat: first",
		"bbbb\x00", // empty subject: must still produce a Commit
		"cccc\x00fix: third",
	}

	got := parseCommits(records)
	require.Equal(t, []Commit{
		{SHA: "aaaa", Subject: "feat: first"},
		{SHA: "bbbb", Subject: ""},
		{SHA: "cccc", Subject: "fix: third"},
	}, got)
}
