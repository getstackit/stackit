package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	syncComponent "github.com/getstackit/stackit/internal/tui/components/sync"
)

func TestRestackScenarioNamesMatchScenarios(t *testing.T) {
	t.Parallel()
	require.Len(t, RestackScenarioNames(), len(restackScenarios))
	for _, name := range RestackScenarioNames() {
		require.Contains(t, restackScenarios, name)
	}
}

func TestRestackScenarioOutcomes(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"success":  "restacked 2, 1 already current",
		"current":  "Everything is up to date",
		"held":     "Restack incomplete: held 1",
		"conflict": "st restack --branch feat/web",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := output.NewTestOutput()
			restackScenarios[name].Replay(stack.NewSimpleSyncHandler(out), 0)
			require.Contains(t, out.String(), want)
		})
	}
}

func TestRestackScenarioInteractiveOutcomes(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"success":  "restacked 2, 1 already current",
		"held":     "Restack incomplete: held 1",
		"conflict": "st restack --branch feat/web",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := tui.NewMockRunner()
			handler := stack.NewInteractiveSyncHandler(runner, syncComponent.NewModel(), output.NewNullOutput(), output.NewNullLogger())
			restackScenarios[name].Replay(handler, 0)
			var summary string
			for _, msg := range runner.Messages() {
				if complete, ok := msg.(syncComponent.CompleteMsg); ok {
					summary = complete.Summary
				}
			}
			require.Contains(t, summary, want)
		})
	}
}
