package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestCommitProjections(t *testing.T) {
	t.Parallel()
	commits := git.Commits{
		{SHA: "full", ShortSHA: "short", Subject: "subject\twith tabs", Message: "subject\n\nbody\n"},
		{SHA: "empty", ShortSHA: "empty", Subject: "", Message: "\n"},
	}
	require.Equal(t, []string{"subject\twith tabs"}, commits.Subjects())
	require.Equal(t, []string{"subject\n\nbody"}, commits.Messages())
	require.Equal(t, []string{"short subject\twith tabs", "empty"}, commits.Onelines())
	require.Len(t, commits, 2, "projections must not discard typed records")
}
