package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// hasRef reports whether a ref exists in the test repo.
func (s *TestShell) hasRef(ref string) bool {
	s.t.Helper()
	s.Git("for-each-ref --format=%(refname) " + ref)
	return strings.TrimSpace(s.Output()) == ref
}

// latestSnapshotCapturedUntracked reports whether the latest snapshot anchored
// an untracked-file capture.
func (s *TestShell) latestSnapshotCapturedUntracked() bool {
	s.t.Helper()
	return s.hasRef("refs/stackit/undo/" + s.GetLatestSnapshotID() + "-untracked")
}

// TestSnapshotCapturesUntrackedOnlyWhenStagingEverything pins down which
// invocations pay for the untracked-file capture. It hashes every untracked
// file in the tree, so it must run only when the command itself is about to
// `git add -A` them into a commit — never on the common path of committing
// what the user already staged.
func TestSnapshotCapturesUntrackedOnlyWhenStagingEverything(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		command string
		want    bool
	}{
		{"create with staged changes", "create b -m 'b'", false},
		{"create --all", "create b --all -m 'b'", true},
		{"modify with staged changes", "modify", false},
		{"modify --update", "modify --update", false},
		{"modify --all", "modify --all", true},
		{"absorb", "absorb --force", false},
		{"absorb --all", "absorb --all --force", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.WriteFile("shared.txt", "one\ntwo\n").Run("create a -m 'a'")

			// A staged edit every command can consume, next to an untracked
			// file only `git add -A` would sweep in.
			sh.WriteFile("shared.txt", "one\nTWO\n").
				WriteUnstaged("scratch.txt", "not for any commit\n")

			before := sh.GetLatestSnapshotID()
			sh.Run(tc.command)
			latest := sh.GetLatestSnapshotID()
			require.NotEqual(t, before, latest, "the command must take its own snapshot")
			require.True(t, sh.hasRef("refs/stackit/undo/"+latest),
				"the staged edit must always be captured")
			require.Equal(t, tc.want, sh.latestSnapshotCapturedUntracked())
		})
	}
}

// TestUndoCreateRestoresStagedNewFileWithoutUntrackedCapture proves the
// tracked capture alone covers files the user staged before running create:
// they are in the index, so the stash holds them, and skipping the untracked
// capture loses nothing.
func TestUndoCreateRestoresStagedNewFileWithoutUntrackedCapture(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)
	sh.WriteFile("base.txt", "base\n").Run("create a -m 'a'")

	sh.WriteFile("fresh.txt", "brand new, staged\n").
		WriteUnstaged("scratch.txt", "untouched\n")
	statusBefore := sh.gitStatus()
	originalA := sh.revParse("a")

	sh.Run("create b -m 'b'")
	require.False(t, sh.latestSnapshotCapturedUntracked())

	sh.Run("undo --snapshot " + sh.GetLatestSnapshotID() + " --yes").
		OutputContains("Restored the uncommitted changes")

	sh.OnBranch("a")
	require.Equal(t, originalA, sh.revParse("a"))
	require.Equal(t, statusBefore, sh.gitStatus(),
		"the staged new file must come back staged, and the untracked file untouched")
	require.Equal(t, "brand new, staged\n", sh.fileContent("fresh.txt"))
	require.Equal(t, "untouched\n", sh.fileContent("scratch.txt"))
}
