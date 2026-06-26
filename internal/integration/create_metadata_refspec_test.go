package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const wantMetadataFetchRefspec = "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*"

// TestCreateConfiguresMetadataRefspec asserts that creating a branch with a
// remote opportunistically configures the metadata fetch refspec, so a plain
// `git fetch` keeps pulling branch metadata. This closes the gap left by
// removing the implicit metadata bootstrap from engine construction (#1330).
func TestCreateConfiguresMetadataRefspec(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t, WithRemote())

	// Fresh state: the metadata refspec is not configured yet.
	require.NotContains(t, fetchRefspecs(t, sh), wantMetadataFetchRefspec)

	sh.Write("a.txt", "a").Run("create feat -m 'feat: a'")

	require.Contains(t, fetchRefspecs(t, sh), wantMetadataFetchRefspec)
}

// TestTrackConfiguresMetadataRefspec asserts the same opportunistic
// configuration happens on track.
func TestTrackConfiguresMetadataRefspec(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t, WithRemote())

	// A plain git branch that stackit does not yet track.
	sh.Git("checkout -b feat").Write("a.txt", "a").Git("commit -m wip").Checkout("main")
	require.NotContains(t, fetchRefspecs(t, sh), wantMetadataFetchRefspec)

	sh.Run("track feat --parent main")

	require.Contains(t, fetchRefspecs(t, sh), wantMetadataFetchRefspec)
}

// TestCreateWithoutRemoteSkipsMetadataRefspec asserts a remote-less repo is not
// polluted with a dangling remote.origin.fetch entry.
func TestCreateWithoutRemoteSkipsMetadataRefspec(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	sh.Write("a.txt", "a").Run("create feat -m 'feat: a'")

	require.NotContains(t, fetchRefspecs(t, sh), wantMetadataFetchRefspec)
}

// fetchRefspecs returns the configured remote.origin.fetch refspecs, or "" when
// no remote is configured (git config exits non-zero, which is expected then).
func fetchRefspecs(t *testing.T, sh *TestShell) string {
	t.Helper()
	out, _ := sh.Scene().Repo.RunGitCommandAndGetOutput("config", "--get-all", "remote.origin.fetch")
	return strings.TrimSpace(out)
}
