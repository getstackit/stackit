package actions_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestModifyAction_Stdin(t *testing.T) {
	t.Run("reads commit message from stdin in non-interactive mode", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a branch with a commit to modify
		s.CreateBranch("feature")
		s.CommitChange("file1", "original message")

		// Create a change to stage for amendment
		err := s.Scene.Repo.CreateChange("staged content", "test-file", false)
		require.NoError(t, err)

		// Mock stdin
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()
		r, w, _ := os.Pipe()
		os.Stdin = r

		expectedMessage := "feat: modified message from stdin"
		go func() {
			_, _ = w.Write([]byte(expectedMessage))
			_ = w.Close()
		}()

		// Scenario already calls tui.SetInteractive(false)

		opts := actions.ModifyOptions{
			All: true,
		}
		err = actions.ModifyAction(s.Context, opts)
		require.NoError(t, err)

		// Verify commit message was updated
		commits, err := s.Scene.Repo.ListCurrentBranchCommitMessages()
		require.NoError(t, err)
		require.Contains(t, commits, expectedMessage)
		require.NotContains(t, commits, "original message")
	})
}
