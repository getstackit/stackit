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
	scenario, found := LookupSyncScenario("success")
	require.True(t, found)

	out := output.NewTestOutput()
	scenario.Replay(stack.NewSimpleSyncHandler(out), 0)
	got := ansi.Strip(out.String())

	assert.Contains(t, got, "main fast-forwarded to a1b2c3d")
	assert.Contains(t, got, "Restacked feat/web (PR #43) on feat/api → d4e5f6a")
	assert.Contains(t, got, "✅ Summary: pulled trunk, synced 1 branch, restacked 1, deleted 1")
}

func TestLookupSyncScenario(t *testing.T) {
	assert.Equal(t, []string{"success", "diverged", "current"}, SyncScenarioNames())
	_, found := LookupSyncScenario("missing")
	assert.False(t, found)
}
