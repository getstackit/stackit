package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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

	t.Run("prunes oldest snapshots beyond configured depth", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Cap retained snapshots at 2 so a handful of operations trip the
		// prune-oldest path (each create takes a snapshot).
		sh.Git("config --local stackit.undo.depth 2")

		sh.Write("f1", "c1").Run("create feat-1 -m 'feat 1'")
		sh.Write("f2", "c2").Run("create feat-2 -m 'feat 2'")
		sh.Write("f3", "c3").Run("create feat-3 -m 'feat 3'")
		sh.Write("f4", "c4").Run("create feat-4 -m 'feat 4'")

		// More than `depth` creates must leave only `depth` snapshots on disk.
		snapshotDir := filepath.Join(sh.Dir(), ".git", "stackit", "undo")
		entries, err := os.ReadDir(snapshotDir)
		require.NoError(t, err)

		jsonCount := 0
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
				jsonCount++
			}
		}
		require.Equal(t, 2, jsonCount, "snapshots should be pruned to the configured undo depth")
	})
}
