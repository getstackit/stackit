package main

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/output"
)

func TestSyncScenarioReplayUsesProductionHandler(t *testing.T) {
	scenario, found := LookupSyncScenario("large-stack")
	require.True(t, found)

	out := output.NewTestOutput()
	scenario.Replay(stack.NewSimpleSyncHandler(out), 0)
	got := ansi.Strip(out.String())

	// An already-current trunk is not reported: only movement gets a row.
	assert.NotContains(t, got, "main is up to date")
	assert.NotContains(t, got, "Pulling from remote")
	assert.Contains(t, got, "Refreshed PR info for 6 branches")
	assert.Contains(t, got, "2 branches landed and cleaned up")
	assert.Contains(t, got, "stack-merge-stack-1784862381")
	// A successful restack is a count, not a row.
	assert.Contains(t, got, "Restacked 5 branches")
	assert.Contains(t, got, "⚠ add-prompt-notes-to-track-LLM-context-on-commits #754 conflict")
	assert.Contains(t, got, "Restacked 5, deleted 2, skipped 1 (conflict)")
	assert.Contains(t, got, "st restack jonnii/20260220125253/add-prompt-notes-to-track-LLM-context-on-commits")
}

func TestLookupSyncScenario(t *testing.T) {
	assert.Equal(t, []string{"success", "large-stack", "diverged", "current"}, SyncScenarioNames())
	_, found := LookupSyncScenario("missing")
	assert.False(t, found)
}
