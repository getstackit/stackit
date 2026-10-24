package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/handlers"
)

// A held branch returns RestackUnneeded — the same status as a branch that
// needed no work — so without an explicit reason "I protected your work" and
// "there was nothing to do" are indistinguishable. The remedy lives in another
// worktree the user is not looking at, so the reason has to name it.
//
// See the "Hold Trunk Never, Report Holds Always" invariant in
// .claude/rules/worktree-safety.md.

// TestModifyReportsHeldBranchInsteadOfUpToDate covers the engine hold reaching
// the user through modify, which restacks upstack branches after amending.
func TestModifyReportsHeldBranchInsteadOfUpToDate(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Park b in a plain git worktree and leave uncommitted tracked work there,
	// so restacking b would have to reset over it.
	worktreeDir := t.TempDir()
	sh.Checkout("a")
	sh.Git("worktree add " + worktreeDir + " b")
	sh.InWorktree(worktreeDir).WriteUnstaged("b.txt_test.txt", "work in progress")

	sh.Write("a_extra", "more work on a").
		Run("modify -a -n").
		OutputContains("Held").
		OutputContains(worktreeDir).
		OutputContains("uncommitted changes")
}

// parkTrunkInWorktree leaves the shell on a stack branch (freeing trunk from
// this checkout), parks trunk in a plain git worktree, and puts the remote one
// commit ahead of local trunk so sync has a fast-forward to attempt. Returns the
// worktree directory holding trunk.
//
// "trunk_tracked.txt" is committed on trunk so callers can dirty a tracked path;
// the incoming remote commit writes only "remote_only.txt", so an untracked file
// at any other path is one a reset could not destroy.
func parkTrunkInWorktree(t *testing.T, sh *TestShell) string {
	t.Helper()

	sh.WriteFile("trunk_tracked.txt", "base").
		Git("commit -m 'chore: trunk base'").
		Git("push -u origin main").
		WriteFile("remote_only.txt", "remote work").
		Git("commit -m 'chore: remote trunk commit'").
		Git("push origin main").
		Git("reset --hard HEAD~1")

	// Moving onto a stack branch frees trunk so a worktree can hold it.
	sh.Write("a.txt", "content for a").Run("create a -m 'Add a'")

	worktreeDir := t.TempDir()
	sh.Git("worktree add " + worktreeDir + " main")
	return worktreeDir
}

// TestSyncProceedsWhenTrunkWorktreeHasHarmlessUntrackedFile pins the boundary
// that broke sync: an untracked file in the worktree holding trunk is the
// ordinary state of having written a new file and not staged it. A reset cannot
// destroy it unless the incoming commit writes that same path, so it must not
// stop the fast-forward — let alone abort the whole command.
func TestSyncProceedsWhenTrunkWorktreeHasHarmlessUntrackedFile(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t, WithRemote())
	worktreeDir := parkTrunkInWorktree(t, sh)

	// Not a path the incoming commit writes.
	sh.InWorktree(worktreeDir).WriteUnstaged("scratch-notes.txt", "notes")

	sh.Run("sync").
		OutputContains("fast-forwarded").
		OutputNotContains("Held")
}

// TestSyncReportsTrunkHoldInsteadOfFailing covers the other half: a tracked
// change in the trunk worktree genuinely blocks the ref move, but that is a hold
// to report, not a failure to abort on. Cleanup and restack do not depend on
// trunk having moved.
func TestSyncReportsTrunkHoldInsteadOfFailing(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t, WithRemote())
	worktreeDir := parkTrunkInWorktree(t, sh)

	// An unstaged edit to a tracked path — a reset would discard it.
	sh.InWorktree(worktreeDir).WriteUnstaged("trunk_tracked.txt", "work in progress")

	// Run, not RunExpectError: the hold must not fail the command, and the
	// summary must not claim it finished.
	sh.Run("sync").
		OutputContains("Held").
		OutputContains(worktreeDir).
		OutputContains("uncommitted changes").
		OutputContains("Sync incomplete: held 1 (worktree)").
		OutputNotContains("Everything is up to date")
}

// TestRestackReportsHeldDescendantInsteadOfUpToDate covers a descendant held
// because its ancestor is: the reason must name the ancestor and carry the
// originating worktree, not report the descendant as up to date.
func TestRestackReportsHeldDescendantInsteadOfUpToDate(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.CreateLinearStack3()

	// Trunk advances so the whole stack genuinely needs restacking.
	sh.Checkout("main").
		WriteFile("trunk1.txt", "trunk content").
		Git("commit -m 'chore: trunk commit'")

	// b is held by its own dirty worktree; c is held only because b is.
	worktreeDir := t.TempDir()
	sh.Git("worktree add " + worktreeDir + " b")
	sh.InWorktree(worktreeDir).WriteUnstaged("b.txt_test.txt", "work in progress")

	sh.Checkout("a").Run("restack --upstack").
		OutputContains("Held").
		OutputContains(worktreeDir)

	// c must not be silently counted as already current.
	sh.OutputNotContains("c up to date")
}

// TestRestackReportsPlanTimeHoldAsIncomplete covers holds that restack applies
// while planning (skipDirtyWorktreeStacks). The pruned branch never reaches the
// engine, so it produces no per-branch event; the summary is the only place the
// hold can surface. Before it did, a dirty worktree on mid-stack b printed a
// "Skipping b" warning followed by "Everything is up to date!" and a JSON
// "success" with no held entry.
func TestRestackReportsPlanTimeHoldAsIncomplete(t *testing.T) {
	t.Parallel()

	// Plain `git worktree` checkout of a mid-stack branch: only b is held.
	t.Run("plain git worktree holds one branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		worktreeDir := t.TempDir()
		sh.Checkout("a")
		sh.Git("worktree add " + worktreeDir + " b")
		sh.InWorktree(worktreeDir).WriteUnstaged("b.txt_test.txt", "work in progress")

		sh.Run("restack --all-stacks").
			OutputContains("Skipping b").
			OutputContains("Restack incomplete: held 1 (worktree)").
			OutputNotContains("Everything is up to date").
			OutputNotContains("Summary:")

		sh.Run("restack --all-stacks --json")
		result := decodeRestackJSON(t, sh.Output())
		require.Equal(t, []handlers.RestackHeldInfo{{
			Branch: "b",
			Reason: "worktree " + worktreeDir + " has uncommitted changes",
		}}, result.Held)
		require.Contains(t, result.Skipped, "b")
	})

	// Stackit-managed worktree: its whole stack is held, which empties the
	// plan. That is an incomplete restack, not "No branches to restack."
	t.Run("managed worktree holds its stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.SetWorktreeBasePath(t.TempDir())

		sh.Run("worktree create my-wt")
		sh.Run("worktree open my-wt")
		worktreePath := strings.TrimSpace(sh.Output())
		anchor := findWorktreeAnchor(t, sh)
		sh.InWorktree(worktreePath).WriteFile("dirty.txt", "uncommitted work")

		sh.Checkout("main").
			Run("restack --all-stacks").
			OutputContains("Skipping stack rooted at").
			OutputContains("Restack incomplete: held 1 (worktree)").
			OutputNotContains("No branches to restack").
			OutputNotContains("Everything is up to date")

		sh.Run("restack --all-stacks --json")
		result := decodeRestackJSON(t, sh.Output())
		require.Len(t, result.Held, 1)
		require.Equal(t, anchor, result.Held[0].Branch)
		require.Contains(t, result.Held[0].Reason, worktreePath)
	})

	// Trunk is never held: a dirty trunk checkout must not make a restack of
	// trunk-rooted branches incomplete.
	t.Run("dirty trunk worktree holds nothing", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()
		sh.Checkout("main").
			WriteFile("trunk1.txt", "trunk content").
			Git("commit -m 'chore: trunk commit'").
			WriteUnstaged("trunk1.txt", "work in progress on trunk")

		sh.Run("restack --all-stacks").
			OutputContains("Summary: restacked 3").
			OutputNotContains("held").
			OutputNotContains("incomplete")
	})
}

func decodeRestackJSON(t *testing.T, out string) handlers.RestackJSONResult {
	t.Helper()
	// Plan-time hold warnings precede the JSON document.
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON object in output: %s", out)
	var result handlers.RestackJSONResult
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &result))
	return result
}
