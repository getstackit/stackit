package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestScopeCommand(t *testing.T) {
	t.Parallel()

	t.Run("scope set fails on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Try to set scope on trunk (main)
		output, err := s.RunCliAndGetOutput("scope", "PROJ-123")
		require.Error(t, err, "scope set should fail on trunk")
		require.Contains(t, output, "cannot set scope on trunk")
	})

	t.Run("scope unset fails on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Try to unset scope on trunk (main)
		output, err := s.RunCliAndGetOutput("scope", "--unset")
		require.Error(t, err, "scope unset should fail on trunk")
		require.Contains(t, output, "cannot unset scope on trunk")
	})

	t.Run("scope show fails on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, func(sc *testhelpers.Scene) error {
			// Create initial commit
			return sc.Repo.CreateChangeAndCommit("initial", "init")
		}).WithInProcess(true)

		// Try to show scope on trunk (main)
		output, err := s.RunCliAndGetOutput("scope", "--show")
		require.Error(t, err, "scope show should fail on trunk")
		require.Contains(t, output, "not on a branch")
	})
}
