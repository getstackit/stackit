package main

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/output"
)

func TestSyncScenarioReplayUsesProductionHandler(t *testing.T) {
	t.Parallel()
	scenario, found := LookupSyncScenario("large-stack")
	require.True(t, found)

	out := output.NewTestOutput()
	scenario.Replay(stack.NewSimpleSyncHandler(out), 0)
	got := ansi.Strip(out.String())

	require.Contains(t, got, "main is up to date")
	require.Contains(t, got, "Updated PR info for 6 branches")
	require.Contains(t, got, "Deleted stack-merge-stack-1784862381 merged into main")
	require.Contains(t, got, "Restacked info-query-cli-rendering (PR #936) on main → 9e49378")
	require.Contains(t, got, "Skipped jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits (PR #754) (conflict)")
	require.Contains(t, got, "✅ Summary: restacked 5, deleted 2, skipped 1 (conflict)")
	require.Contains(t, got, "Run st restack jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits to resolve and continue")
}

func TestLookupSyncScenario(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"success", "large-stack", "diverged", "current"}, SyncScenarioNames())
	_, found := LookupSyncScenario("missing")
	require.False(t, found)
}
