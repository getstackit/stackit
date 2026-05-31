package integration

import (
	"testing"
)

func TestMoveMarksPRBodyUpdateFlagPersists(t *testing.T) {
	t.Parallel()
	t.Run("flag persists across log and info commands", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "a").Run("create feature-a -m 'feat: feature A'")
		sh.Write("b", "b").Run("create feature-b -m 'feat: feature B'")

		sh.Run("move feature-b --onto main --yes")

		sh.CommitCount("main", "feature-b", 1)
		sh.ExpectNeedsPRBodyUpdate("feature-b", true)
		sh.ExpectNeedsPRBodyUpdate("feature-a", true)

		// log and info must not clear the flag.
		sh.Run("log")
		sh.Run("info")

		sh.ExpectNeedsPRBodyUpdate("feature-b", true)
		sh.ExpectNeedsPRBodyUpdate("feature-a", true)
	})
}
