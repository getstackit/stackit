package common

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestCommandPhase(t *testing.T) {
	t.Parallel()

	build := func() *cobra.Command {
		root := &cobra.Command{Use: "stackit"}
		modify := &cobra.Command{Use: "modify"}
		root.AddCommand(modify)
		wt := &cobra.Command{Use: "worktree"}
		wtCreate := &cobra.Command{Use: "create"}
		wt.AddCommand(wtCreate)
		root.AddCommand(wt)
		return root
	}

	tests := []struct {
		name     string
		path     []string // walk from root by Use
		phase    PhasePrefix
		expected string
	}{
		{name: "pre-modify on top-level command", path: []string{"modify"}, phase: PhasePre, expected: "pre-modify"},
		{name: "post-modify on top-level command", path: []string{"modify"}, phase: PhasePost, expected: "post-modify"},
		{name: "nested worktree create", path: []string{"worktree", "create"}, phase: PhasePost, expected: "post-worktree-create"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := build()
			cmd := root
			for _, segment := range tt.path {
				next, _, err := cmd.Find([]string{segment})
				assert.NoError(t, err)
				cmd = next
			}
			assert.Equal(t, tt.expected, CommandPhase(cmd, tt.phase))
		})
	}
}
