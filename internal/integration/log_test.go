package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLogTrunkHistory covers the stack-aware `stackit log` command end-to-end.
// Collapse/enrichment behavior is unit-tested in internal/git and
// internal/actions/trunklog; this verifies CLI wiring, JSON shape, and arg
// parsing in-process (offline, no GitHub).
func TestLogTrunkHistory(t *testing.T) {
	t.Parallel()

	t.Run("log renders trunk history without error", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Run("log")
	})

	t.Run("log --json emits parseable commits", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Run("log --json")

		var result struct {
			Commits []struct {
				SHA     string `json:"sha"`
				Message string `json:"message"`
				Kind    string `json:"kind"`
			} `json:"commits"`
		}
		require.NoError(t, json.Unmarshal([]byte(sh.Output()), &result), "log --json should produce valid JSON")
		require.GreaterOrEqual(t, len(result.Commits), 1, "trunk should have at least one commit")
		require.NotEmpty(t, result.Commits[0].SHA)
		require.Equal(t, "regular", result.Commits[0].Kind)
	})

	t.Run("log rejects a non-range argument", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.RunExpectError("log foo").OutputContains("expected a range")
	})
}
