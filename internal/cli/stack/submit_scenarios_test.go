package stack

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/output"
)

func TestSubmitScenarioReplayUsesProductionHandler(t *testing.T) {
	scenario, found := LookupSubmitScenario("success")
	require.True(t, found)

	out := output.NewTestOutput()
	scenario.Replay(NewSimpleSubmitHandler(out), 0)
	got := ansi.Strip(out.String())

	assert.Contains(t, got, "Submitting 2 branches")
	assert.Contains(t, got, "feat/api #42 updated")
	assert.Contains(t, got, "feat/web #43 created")
	assert.Contains(t, got, "could not apply reviewer @octo")
}

func TestLookupSubmitScenario(t *testing.T) {
	assert.Equal(t, []string{"success", "current", "ss"}, SubmitScenarioNames())
	_, found := LookupSubmitScenario("missing")
	assert.False(t, found)
}

func TestSubmitScenarioSSReplay(t *testing.T) {
	scenario, found := LookupSubmitScenario("ss")
	require.True(t, found)

	out := output.NewTestOutput()
	scenario.Replay(NewSimpleSubmitHandler(out), 0)
	got := ansi.Strip(out.String())

	assert.Contains(t, got, "Will submit (1)")
	assert.Contains(t, got, "No changes (1)")
	assert.Contains(t, got, "add-sync-TUI-lab")
	assert.Contains(t, got, "add-submit-TUI-lab-scenarios #1417 created")
	assert.Contains(t, got, "✓ 1 created, 1 unchanged (5.5s)")
}
