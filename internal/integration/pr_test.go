package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/utils"
	"github.com/getstackit/stackit/testhelpers"
)

// withCapturedBrowser overrides utils.OpenBrowser for the duration of a test
// so the pr command doesn't shell out to a real browser launcher. It returns
// a pointer to the URL that was "opened".
//
// Callers must not run this subtest with t.Parallel(): utils.OpenBrowser is a
// package-level var, so concurrent overrides would race.
func withCapturedBrowser(t *testing.T) *string {
	t.Helper()
	opened := new(string)
	orig := utils.OpenBrowser
	utils.OpenBrowser = func(url string) error {
		*opened = url
		return nil
	}
	t.Cleanup(func() { utils.OpenBrowser = orig })
	return opened
}

// setPrInfo writes PR metadata for a branch directly to the repo at dir,
// independent of the TestShell's own (already-built) engine instance.
func setPrInfo(t *testing.T, dir, branch string, prInfo *engine.PrInfo) {
	t.Helper()
	eng, err := engine.NewEngine(engine.Options{RepoRoot: dir, Trunk: "main"})
	require.NoError(t, err)
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch(branch), prInfo))
}

// TestPRCommand verifies that `stackit pr` resolves the right pull request
// and opens it in the browser.
func TestPRCommand(t *testing.T) {
	t.Parallel()

	t.Run("opens the current branch's PR", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		setPrInfo(t, sh.Dir(), "c", testhelpers.NewTestPrInfoFull(3, "", "", "OPEN", "b", "https://github.com/o/r/pull/3", false))

		sh.Run("pr")
		require.Equal(t, "https://github.com/o/r/pull/3", *opened)
	})

	t.Run("opens an explicit branch's PR", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		setPrInfo(t, sh.Dir(), "a", testhelpers.NewTestPrInfoFull(1, "", "", "OPEN", "main", "https://github.com/o/r/pull/1", false))

		sh.Run("pr a")
		require.Equal(t, "https://github.com/o/r/pull/1", *opened)
	})

	t.Run("--stack opens the root PR of the stack", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		setPrInfo(t, sh.Dir(), "a", testhelpers.NewTestPrInfoFull(1, "", "", "OPEN", "main", "https://github.com/o/r/pull/1", false))

		sh.Run("pr c --stack")
		require.Equal(t, "https://github.com/o/r/pull/1", *opened)
	})

	t.Run("errors with an actionable message when there is no PR", func(t *testing.T) {
		withCapturedBrowser(t)
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()

		sh.RunExpectError("pr").
			OutputContains("has no associated pull request")
	})
}
