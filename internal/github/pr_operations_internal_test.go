package github

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsMergeStateStatusUnsupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "GitHub Enterprise field-missing error",
			err:  errors.New("GraphQL error: Field 'mergeStateStatus' doesn't exist on type 'PullRequest'"),
			want: true,
		},
		{
			name: "wrapped error mentioning the field",
			err:  errors.New("failed to get PR mergeable state: GraphQL error: field mergeStateStatus doesn't exist"),
			want: true,
		},
		{
			name: "unrelated GraphQL error",
			err:  errors.New("GraphQL error: Could not resolve to a node with the global id"),
			want: false,
		},
		{
			name: "network error",
			err:  errors.New("failed to execute GraphQL request: connection refused"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isMergeStateStatusUnsupported(tt.err))
		})
	}
}

func TestMergeableToMergeStateText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mergeable string
		want      string
	}{
		{
			name:      "MERGEABLE maps to CLEAN",
			mergeable: "MERGEABLE",
			want:      "CLEAN",
		},
		{
			name:      "CONFLICTING maps to DIRTY",
			mergeable: "CONFLICTING",
			want:      "DIRTY",
		},
		{
			name:      "UNKNOWN maps to UNKNOWN",
			mergeable: "UNKNOWN",
			want:      "UNKNOWN",
		},
		{
			name:      "empty maps to UNKNOWN",
			mergeable: "",
			want:      "UNKNOWN",
		},
		{
			name:      "unexpected value maps to UNKNOWN",
			mergeable: "SOMETHING_ELSE",
			want:      "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mergeableToMergeStateText(tt.mergeable))
		})
	}
}
