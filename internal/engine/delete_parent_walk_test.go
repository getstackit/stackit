package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNearestSurvivingParent covers the walk that resolves a surviving child to
// the nearest parent that outlives a batch deletion.
//
// The walk followed Parent links with no step limit, so a parent loop among the
// branches being deleted — corrupt metadata, or metadata rewritten by another
// stackit process mid-delete — spun forever and hung the whole deletion.
func TestNearestSurvivingParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		start    string
		parentOf map[string]string
		toDelete []string
		expected string
	}{
		{
			name:     "parent survives",
			start:    "keep",
			parentOf: map[string]string{"keep": "main"},
			toDelete: []string{"gone"},
			expected: "keep",
		},
		{
			name:     "walks past a chain of deleted parents",
			start:    "d1",
			parentOf: map[string]string{"d1": "d2", "d2": "d3", "d3": "keep", "keep": "main"},
			toDelete: []string{"d1", "d2", "d3"},
			expected: "keep",
		},
		{
			name:     "two-branch cycle falls back to trunk",
			start:    "a",
			parentOf: map[string]string{"a": "b", "b": "a"},
			toDelete: []string{"a", "b"},
			expected: "main",
		},
		{
			name:     "self-parent falls back to trunk",
			start:    "a",
			parentOf: map[string]string{"a": "a"},
			toDelete: []string{"a"},
			expected: "main",
		},
		{
			name:     "missing parent metadata resolves to the empty parent",
			start:    "orphan",
			parentOf: map[string]string{},
			toDelete: []string{"orphan"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toDelete := make(map[string]bool, len(tt.toDelete))
			for _, name := range tt.toDelete {
				toDelete[name] = true
			}
			require.Equal(t, tt.expected, nearestSurvivingParent(tt.start, tt.parentOf, toDelete, "main"))
		})
	}
}
