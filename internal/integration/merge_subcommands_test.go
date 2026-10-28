package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// refStateSnapshot captures every ref with the object it points at, plus HEAD, so
// a sequence of read-only commands on a shared shell can prove it did not
// mutate the repository. Undo snapshots are excluded: some commands record one
// before a validation refusal, which leaves branch state untouched.
func refStateSnapshot(sh *TestShell) string {
	sh.t.Helper()
	sh.Git("for-each-ref --format=%(refname)=%(objectname)")
	var refs strings.Builder
	for _, line := range strings.SplitAfter(sh.Output(), "\n") {
		if !strings.HasPrefix(line, "refs/stackit/undo/") {
			refs.WriteString(line)
		}
	}
	sh.Git("rev-parse --abbrev-ref HEAD")
	return refs.String() + "HEAD=" + sh.Output()
}

// requireReadOnly runs fn, which executes and asserts on commands that must not
// mutate the repository, and fails if any ref or HEAD moved.
func requireReadOnly(sh *TestShell, fn func()) {
	sh.t.Helper()
	before := refStateSnapshot(sh)
	fn()
	require.Equal(sh.t, before, refStateSnapshot(sh), "read-only commands changed repository refs")
}

// setOpenPRs records an OPEN PR numbered 101, 102, ... for each branch in order.
func setOpenPRs(sh *TestShell, branches ...string) {
	sh.t.Helper()
	for i, branch := range branches {
		sh.SetPrMetadata(branch, PRMetadata{Number: 101 + i, State: "OPEN"})
	}
}

// TestMergeEmptyRepo covers merge commands on a repository with no stacks,
// --help output, multi-stack ship, and untracked branches. Every command before
// the untracked case is read-only, so they share one shell.
func TestMergeEmptyRepo(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	sh := NewTestShellInProcess(t)

	t.Run("no stacks", func(t *testing.T) {
		sh := sh.WithT(t)
		requireReadOnly(sh, func() {
			sh.Run("merge status").
				OutputContains("No active stacks found")

			sh.OnBranch("main")
			for _, cmd := range []string{"merge next --dry-run", "merge drain --dry-run", "merge ship --dry-run"} {
				sh.RunExpectError(cmd).OutputContains("cannot merge from trunk")
			}
		})
	})

	t.Run("help", func(t *testing.T) {
		sh := sh.WithT(t)
		requireReadOnly(sh, func() {
			// Parent help lists the subcommands.
			sh.Run("merge --help").
				OutputContains("status").
				OutputContains("next").
				OutputContains("drain").
				OutputContains("ship")

			for _, tt := range []struct {
				cmd     string
				contain string
			}{
				{"merge next --help", "bottom-most"},
				{"merge ship --help", "Consolidate"},
				{"merge drain --help", "bottom-up"},
				{"merge status --help", "ready to merge"},
			} {
				sh.Run(tt.cmd).OutputContains(tt.contain)
			}
		})
	})

	t.Run("multi-stack dry-run shows plan without executing", func(t *testing.T) {
		sh := sh.WithT(t)
		// Create two independent stacks rooted at trunk
		sh.Write("a.txt", "content-a").
			Run("create stack-a -m 'Add stack-a'")
		sh.Checkout("main")
		sh.Write("b.txt", "content-b").
			Run("create stack-b -m 'Add stack-b'")

		requireReadOnly(sh, func() {
			sh.Run("merge ship --stacks stack-a,stack-b --dry-run").
				OutputContains("Dry-run mode").
				OutputContains("stack-a").
				OutputContains("stack-b")
		})
	})

	t.Run("errors when branch is not tracked", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Checkout("main")
		sh.Git("checkout -b untracked-branch")
		sh.Write("untracked.txt", "content")
		sh.Git("add -A")
		sh.Git("commit -m 'untracked commit'")

		sh.RunExpectError("merge next --dry-run").
			OutputContains("not tracked")
	})
}

// TestMergeSingleBranch covers merge commands on a one-branch stack: the
// no-PR and non-interactive errors first, then flag variants once the PR exists.
func TestMergeSingleBranch(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	sh := NewTestShellInProcess(t)
	sh.Write("a.txt", "content-a").Run("create branch-a -m 'Add branch-a'")

	t.Run("without a PR", func(t *testing.T) {
		sh := sh.WithT(t)
		requireReadOnly(sh, func() {
			// merge (without subcommand) in non-interactive mode requires --yes.
			sh.RunExpectError("merge").
				OutputContains("pass --yes to merge the next").
				OutputContains("requires a TTY")

			for _, cmd := range []string{"merge next --dry-run", "merge drain --dry-run", "merge ship --dry-run"} {
				sh.RunExpectError(cmd).OutputContains("no open PRs")
			}
		})
	})

	t.Run("with an open PR", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 101, State: "OPEN"})

		requireReadOnly(sh, func() {
			for _, cmd := range []string{
				"merge next --yes --dry-run",
				"merge next --force --dry-run",
				"merge next --dry-run",
			} {
				sh.Run(cmd).OutputContains("branch-a")
			}

			// ship accepts --yes and the squash alias
			for _, cmd := range []string{
				"merge ship --yes --dry-run",
				"merge ship --dry-run",
				"merge squash --dry-run",
			} {
				sh.Run(cmd).OutputContains("Consolidate")
			}

			sh.RunExpectError("merge drain --count -1 --dry-run").
				OutputContains("--count must be non-negative")

			sh.RunExpectError("merge ship --scope nonexistent --dry-run").
				OutputContains("no branches found")
		})
	})

	t.Run("status shows the stack", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.SetPrMetadata("branch-a", PRMetadata{
			Number: 101,
			State:  "OPEN",
			URL:    "https://github.com/owner/repo/pull/101",
		})
		requireReadOnly(sh, func() {
			// Incomplete since no approvals/CI, but the stack is listed.
			sh.Run("merge status --all").
				OutputContains("branch-a")
		})
	})

	t.Run("shows draft PR in plan", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.SetPrMetadata("branch-a", PRMetadata{
			Number: 101,
			State:  "OPEN",
			Draft:  true,
			URL:    "https://github.com/owner/repo/pull/101",
		})
		requireReadOnly(sh, func() {
			// Dry-run shows the plan (draft check happens during actual merge)
			sh.Run("merge next --dry-run").
				OutputContains("branch-a").
				OutputContains("#101")
		})
	})
}

// TestMergeTwoBranchStack keeps a genuine 2-PR stack for the drain totals.
func TestMergeTwoBranchStack(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)
	sh.Write("a.txt", "content-a").
		Run("create branch-a -m 'Add branch-a'")
	sh.Write("b.txt", "content-b").
		Run("create branch-b -m 'Add branch-b'")
	setOpenPRs(sh, "branch-a", "branch-b")

	requireReadOnly(sh, func() {
		sh.Run("merge drain --dry-run").
			OutputContains("Total: 2 PRs to drain")

		// --count=10 on 2-PR stack: drains all, no limiting message
		sh.Run("merge drain --count 10 --dry-run").
			OutputNotContains("Draining first")
	})
}

// TestMergeThreeBranchStack runs the dry-run plans for a three-PR stack on one
// shell, then flips PR states to cover merged and closed PRs.
func TestMergeThreeBranchStack(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	sh := NewTestShellInProcess(t)
	sh.Write("a.txt", "content-a").
		Run("create branch-a -m 'Add branch-a'")
	sh.Write("b.txt", "content-b").
		Run("create branch-b -m 'Add branch-b'")
	sh.Write("c.txt", "content-c").
		Run("create branch-c -m 'Add branch-c'")
	sh.SetPrMetadata("branch-a", PRMetadata{Number: 101, State: "OPEN", URL: "https://github.com/owner/repo/pull/101"})
	sh.SetPrMetadata("branch-b", PRMetadata{Number: 102, State: "OPEN", URL: "https://github.com/owner/repo/pull/102"})
	sh.SetPrMetadata("branch-c", PRMetadata{Number: 103, State: "OPEN", URL: "https://github.com/owner/repo/pull/103"})

	t.Run("plans from the top of the stack", func(t *testing.T) {
		sh := sh.WithT(t)
		requireReadOnly(sh, func() {
			sh.Run("merge drain --dry-run").
				OutputContains("drain").
				OutputContains("branch-a").
				OutputContains("branch-b").
				OutputContains("branch-c").
				OutputContains("#101").
				OutputContains("#102").
				OutputContains("#103").
				OutputContains("Dry-run mode")

			sh.Run("merge drain --count 1 --dry-run").
				OutputContains("Draining first 1 of 3").
				OutputContains("Dry-run mode")

			sh.Run("merge ship --dry-run").
				OutputContains("Consolidate").
				OutputContains("branch-a").
				OutputContains("branch-b").
				OutputContains("branch-c").
				OutputContains("Dry-run mode")

			// --force bypasses validation errors and still shows the plan
			sh.Run("merge ship --force --dry-run").
				OutputContains("Consolidate").
				OutputContains("Dry-run mode")

			// next targets the bottom PR and mentions restacking upstack branches
			sh.Run("merge next --dry-run").
				OutputContains("branch-a").
				OutputContains("#101").
				OutputContains("Dry-run mode").
				OutputContains("Restack")

			// Parent `merge --yes --dry-run` behaves like `merge next --dry-run`:
			// target the bottom-most PR (branch-a / #101), not consolidate.
			sh.Run("merge --yes --dry-run").
				OutputContains("branch-a").
				OutputContains("#101").
				OutputContains("Dry-run mode")
		})
	})

	t.Run("plans from mid-stack and from trunk with --branch", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Checkout("branch-b")
		requireReadOnly(sh, func() {
			// Should still find branch-a as bottom
			sh.Run("merge next --dry-run").
				OutputContains("branch-a")
		})

		sh.Checkout("main")
		requireReadOnly(sh, func() {
			// Should find branch-a as bottom of branch-b's stack
			sh.Run("merge next --branch branch-b --dry-run").
				OutputContains("branch-a").
				OutputContains("#101")

			sh.Run("merge drain --branch branch-b --dry-run").
				OutputContains("branch-a").
				OutputContains("branch-b").
				OutputContains("Dry-run mode")
		})
	})

	t.Run("skips merged PRs", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Checkout("branch-c")
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 101, State: "MERGED"})
		requireReadOnly(sh, func() {
			sh.Run("merge drain --dry-run").
				OutputContains("#102").
				OutputContains("#103").
				OutputNotContains("#101").
				OutputContains("Total: 2 PRs to drain")

			sh.Run("merge next --dry-run").
				OutputContains("branch-b").
				OutputContains("#102").
				OutputNotContains("#101")
		})
	})

	t.Run("does not block on closed upstack PRs", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 101, State: "OPEN"})
		sh.SetPrMetadata("branch-b", PRMetadata{Number: 102, State: "CLOSED"})
		requireReadOnly(sh, func() {
			// Should focus on bottom PR only
			sh.Run("merge next --dry-run").
				OutputContains("Enable automerge for PR #101").
				OutputNotContains("PR #102")
		})
	})
}

func TestMergeNextScopeFlag(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// Set a branch pattern that uses scope
	sh.Run("config set branch.pattern \"{scope}/{message}\"")

	sh.Write("a.txt", "content-a").
		Run("create -m 'branch-a' --scope PROJ-100")
	sh.Write("b.txt", "content-b").
		Run("create -m 'branch-b' --scope PROJ-100")

	sh.SetPrMetadata("PROJ-100/branch-a", PRMetadata{Number: 101, State: "OPEN"})
	sh.SetPrMetadata("PROJ-100/branch-b", PRMetadata{Number: 102, State: "OPEN"})

	sh.Run("merge next --scope PROJ-100 --dry-run").
		OutputContains("#101").
		OutputContains("Dry-run mode")
}
