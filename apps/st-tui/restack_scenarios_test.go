package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/output"
)

func TestRestackScenarioOutcomes(t *testing.T) {
	for name, want := range map[string]string{
		"success":  "restacked 2, 1 already current",
		"current":  "Everything is up to date",
		"held":     "Restack incomplete: held 1",
		"conflict": "st restack --branch feat/web",
	} {
		t.Run(name, func(t *testing.T) {
			out := output.NewTestOutput()
			restackScenarios[name].Replay(stack.NewSimpleSyncHandler(out), 0)
			require.Contains(t, out.String(), want)
		})
	}
}
