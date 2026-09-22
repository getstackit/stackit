package httpcontract

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestMapCommitsPreservesFields(t *testing.T) {
	t.Parallel()
	records := git.Commits{
		{ShortSHA: "abc1234", Subject: "subject\twith tabs", AuthorDate: "2026-01-01T12:00:00+02:00"},
		{ShortSHA: "def5678", Subject: "", AuthorDate: "2026-01-01T11:00:00Z"},
	}
	require.Equal(t, []CommitResponse{
		{SHA: "abc1234", Message: "subject\twith tabs", Date: "2026-01-01T10:00:00Z"},
		{SHA: "def5678", Message: "", Date: "2026-01-01T11:00:00Z"},
	}, mapCommits(records))
}
