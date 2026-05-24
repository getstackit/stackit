package submit

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsRaceConditionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "non-fast-forward rejection",
			err:      errors.New("! [rejected] main -> main (non-fast-forward)"),
			expected: true,
		},
		{
			name:     "fetch first rejection",
			err:      errors.New("! [rejected] main -> main (fetch first)"),
			expected: true,
		},
		{
			name:     "updates were rejected",
			err:      errors.New("error: failed to push some refs\n ! [rejected] main -> main (updates were rejected)"),
			expected: true,
		},
		{
			name:     "needs force rejection",
			err:      errors.New("! [rejected] refs/stackit/metadata/foo -> refs/stackit/metadata/foo (needs force)"),
			expected: true,
		},
		{
			name:     "generic push failure",
			err:      errors.New("failed to push"),
			expected: false,
		},
		{
			name:     "network error",
			err:      errors.New("connection refused"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRaceConditionError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

