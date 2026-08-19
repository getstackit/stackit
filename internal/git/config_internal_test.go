package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeConfigKey pins the key rendering the stackit.* snapshot relies
// on. `git config --get-regexp` lowercases the section and the trailing key
// name but preserves the subsection verbatim; if this drifts, Get silently
// returns "" for a key that is actually set.
func TestNormalizeConfigKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"already lowercase", "stackit.trunk", "stackit.trunk"},
		{"mixed-case key name", "stackit.maxConcurrency", "stackit.maxconcurrency"},
		{"subsection with mixed-case key", "stackit.worktree.basePath", "stackit.worktree.basepath"},
		{"lowercase subsection preserved", "stackit.undo.depth", "stackit.undo.depth"},
		{"subsection case is significant", "branch.Feature-A.remote", "branch.Feature-A.remote"},
		{"subsection may contain dots", "branch.a.b/c.remote", "branch.a.b/c.remote"},
		{"no separator", "nodots", "nodots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeConfigKey(tt.key))
		})
	}
}
