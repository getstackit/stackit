package integration

import (
	"testing"
)

func TestDescribeCommand(t *testing.T) {
	t.Parallel()

	t.Run("description appears in stack info", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature.txt", "content")
		sh.Run("create -m 'root feature'")

		// Use title without spaces to avoid ANSI code splitting issues with glamour
		sh.Run("describe -m 'AuthFeature' -d 'OAuth2 implementation'")
		sh.Run("info --stack").
			OutputContains("AuthFeature"). // Title rendered as markdown heading
			OutputContains("OAuth2")       // Description appears in output
	})

	t.Run("error on trunk", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.RunExpectError("describe -m 'Title'").
			OutputContains("cannot set stack description on trunk")
	})

	t.Run("error on untracked branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Git("checkout -b untracked")
		sh.Write("file.txt", "content")
		sh.Git("commit -m 'commit'")

		sh.RunExpectError("describe -m 'Title'").
			OutputContains("not tracked")
	})
}
