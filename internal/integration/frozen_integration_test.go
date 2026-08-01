package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrozenIntegration(t *testing.T) {
	t.Parallel()

	t.Run("freeze and unfreeze recursive behavior", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		s.Run("create a -m 'feat a'")
		s.Run("create b -m 'feat b'")
		s.Run("create c -m 'feat c'")

		// 1. Freeze b: should freeze b and a (downstack)
		s.Run("freeze b")

		// Check status using st info
		output := s.Run("info a").lastOutput
		require.Contains(t, output, "(frozen)")
		output = s.Run("info b").lastOutput
		require.Contains(t, output, "(frozen)")
		output = s.Run("info c").lastOutput
		require.NotContains(t, output, "(frozen)")

		// 2. Unfreeze a: should unfreeze a, b, and c (upstack)
		s.Run("unfreeze a")
		output = s.Run("info a").lastOutput
		require.NotContains(t, output, "(frozen)")
		output = s.Run("info b").lastOutput
		require.NotContains(t, output, "(frozen)")
		output = s.Run("info c").lastOutput
		require.NotContains(t, output, "(frozen)")
	})

	t.Run("frozen branch blocks modifications", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create a -m 'feat a'")
		s.Run("freeze a")

		// Try to modify
		s.RunExpectError("modify -m 'updated a'")
		require.Contains(t, s.lastOutput, "branch a is frozen. Use 'st unfreeze' to enable modifications")

		// Try to squash
		s.RunExpectError("squash")
		require.Contains(t, s.lastOutput, "branch a is frozen. Use 'st unfreeze' to enable modifications")

		// Try to absorb
		// First create a change
		s.Scene().Repo.CreateChange("new content", "a", false)
		s.RunExpectError("absorb")
		require.Contains(t, s.lastOutput, "branch a is frozen. Use 'st unfreeze' to enable modifications")
	})

	t.Run("locked and frozen branch shows combined error", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t, WithRemote())

		s.Run("create a -m 'feat a'")
		s.Run("freeze a")
		s.Run("lock a")

		// Try to modify
		s.RunExpectError("modify -m 'updated a'")
		require.Contains(t, s.lastOutput, "branch a is locked (user) and frozen. Use 'st unlock' and 'st unfreeze' to enable modifications")
	})

	t.Run("passthrough git commands are blocked", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create a -m 'feat a'")
		s.Run("freeze a")

		// git commit --amend should be blocked by passthrough logic
		s.RunExpectError("commit --amend --no-edit")
		require.Contains(t, s.lastOutput, "branch a is frozen. Use 'st unfreeze' to enable modifications")
	})

	t.Run("freeze fails on trunk branch", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.RunExpectError("freeze main").
			OutputContains("cannot freeze trunk branch")
	})

	t.Run("freeze and unfreeze fail on untracked branch", func(t *testing.T) {
		t.Parallel()
		for _, cmd := range []string{"freeze", "unfreeze"} {
			t.Run(cmd, func(t *testing.T) {
				t.Parallel()
				s := NewTestShellInProcess(t)
				s.Git("checkout -b untracked").
					WriteFile("file", "content").
					Git("add file").
					Git("commit -m 'untracked commit'")

				s.RunExpectError(cmd + " untracked").
					OutputContains("not tracked by stackit")
			})
		}
	})

	t.Run("frozen branch blocks rename", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create feature-a").
			WriteFile("a", "content").
			Git("add a").
			Git("commit -m 'A'")
		s.Run("freeze feature-a")

		s.RunExpectError("rename new-name").
			OutputContains("frozen")
	})

	t.Run("frozen branch blocks split", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		// Create a branch with multiple commits to split
		s.Run("create feature-a").
			WriteFile("a", "content").
			Git("add a").
			Git("commit -m 'A'").
			WriteFile("b", "content").
			Git("add b").
			Git("commit -m 'B'")
		s.Run("freeze feature-a")

		s.RunExpectError("split").
			OutputContains("frozen")
	})

	t.Run("frozen branch in stack blocks reorder", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		// Create a stack
		s.Run("create feature-a").
			WriteFile("a", "content").
			Git("add a").
			Git("commit -m 'A'")
		s.Run("create feature-b").
			WriteFile("b", "content").
			Git("add b").
			Git("commit -m 'B'")

		// Freeze one branch in the stack
		s.Run("freeze feature-a")

		s.RunExpectError("reorder").
			OutputContains("frozen")
	})

	t.Run("frozen status persists after checkout", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create feature-a -m 'feat a'")
		s.Run("freeze feature-a")

		// Verify frozen
		s.Run("info").OutputContains("(frozen)")

		// Checkout away and back
		s.Checkout("main")
		s.Checkout("feature-a")

		// Still frozen
		s.Run("info").OutputContains("(frozen)")
	})

	t.Run("frozen status persists after creating child branch", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create feature-a -m 'feat a'")
		s.Run("freeze feature-a")

		// Create a child branch
		s.Run("create feature-b -m 'feat b'")

		// Parent should still be frozen
		s.Run("info feature-a").OutputContains("(frozen)")

		// Child should not be frozen
		s.Run("info feature-b").OutputNotContains("(frozen)")
	})

	t.Run("remote advance restacks children in the same pass", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		// Stack: main -> f -> c
		s.Run("create f").
			WriteFile("f.txt", "f1").
			Git("commit -m 'F1'")
		s.Run("create c").
			WriteFile("c.txt", "c1").
			Git("commit -m 'C1'")

		// Teammate advances f's remote: build a commit on top of f without
		// moving the local ref, then point origin/f at it.
		s.Git("checkout --detach f").
			WriteFile("f2.txt", "f2").
			Git("commit -m 'F2'").
			Git("update-ref refs/remotes/origin/f HEAD")
		s.Checkout("c")
		s.Run("freeze f")

		s.Run("restack")

		// f must hard-reset to its remote SHA, and c must follow onto the
		// new f in the same restack pass — not report "up to date" against
		// f's stale pre-reset tip.
		remoteSHA := strings.TrimSpace(s.Git("rev-parse refs/remotes/origin/f").lastOutput)
		localSHA := strings.TrimSpace(s.Git("rev-parse f").lastOutput)
		require.Equal(t, remoteSHA, localSHA, "frozen branch must reset to its remote SHA")
		s.Git("merge-base --is-ancestor f c")
	})

	t.Run("unfreeze allows modifications", func(t *testing.T) {
		t.Parallel()
		s := NewTestShellInProcess(t)

		s.Run("create feature-a").
			WriteFile("a", "content").
			Git("add a").
			Git("commit -m 'A'")
		s.Run("freeze feature-a")

		// Verify frozen blocks modify
		s.WriteFile("a", "modified")
		s.RunExpectError("modify -n").OutputContains("frozen")

		// Unfreeze
		s.Run("unfreeze feature-a")

		// Now modify should work
		s.Run("modify -n")
	})
}
