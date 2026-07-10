package split

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTypeChoices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		hasMultipleCommits bool
		wantAvailableCount int
		wantCommitAvail    bool
	}{
		{
			name:               "single commit branch",
			hasMultipleCommits: false,
			wantAvailableCount: 2, // hunk and file
			wantCommitAvail:    false,
		},
		{
			name:               "multiple commit branch",
			hasMultipleCommits: true,
			wantAvailableCount: 3, // hunk, file, and commit
			wantCommitAvail:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			choices := buildTypeChoices(tt.hasMultipleCommits)

			// Check total count
			if len(choices) != 3 {
				t.Errorf("buildTypeChoices() returned %d choices, want 3", len(choices))
			}

			// Count available choices
			availableCount := 0
			var commitChoice *TypeChoice
			for i := range choices {
				if choices[i].Available {
					availableCount++
				}
				if choices[i].Style == StyleCommit {
					commitChoice = &choices[i]
				}
			}

			if availableCount != tt.wantAvailableCount {
				t.Errorf("buildTypeChoices() has %d available choices, want %d",
					availableCount, tt.wantAvailableCount)
			}

			require.NotNil(t, commitChoice, "buildTypeChoices() missing commit choice")
			require.Equal(t, tt.wantCommitAvail, commitChoice.Available,
				"buildTypeChoices() commit.Available")
		})
	}
}

func TestDirection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		direction Direction
		wantStr   string
	}{
		{DirectionBelow, "below"},
		{DirectionAbove, "above"},
		{Direction(""), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.direction), func(t *testing.T) {
			t.Parallel()
			if got := tt.direction.String(); got != tt.wantStr {
				t.Errorf("Direction.String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}
