package integration

import (
	"testing"
)

func TestUndoCommand(t *testing.T) {
	t.Parallel()

	t.Run("undo shows no history message when empty", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content1").
			Commit("file1", "initial commit")

		sh.Run("undo").
			OutputContains("No undo history available")
	})
}
