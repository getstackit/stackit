package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailedTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		action   string
		target   string
		err      error
		expected string
	}{
		{
			name:     "wraps error with action and target",
			action:   "get",
			target:   "branch revision",
			err:      errors.New("ref not found"),
			expected: "failed to get branch revision: ref not found",
		},
		{
			name:     "returns nil for nil error",
			action:   "get",
			target:   "branch",
			err:      nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FailedTo(tt.action, tt.target, tt.err)
			if tt.err == nil {
				require.Nil(t, result)
			} else {
				require.Equal(t, tt.expected, result.Error())
			}
		})
	}
}

func TestErrorUnwrapping(t *testing.T) {
	t.Parallel()

	originalErr := errors.New("original error")

	t.Run("FailedTo preserves wrapped error", func(t *testing.T) {
		t.Parallel()
		wrapped := FailedTo("get", "branch", originalErr)
		require.ErrorIs(t, wrapped, originalErr)
	})
}
