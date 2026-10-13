package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnique(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{name: "nil input yields empty result", input: nil, expected: []string{}},
		{name: "no duplicates unchanged", input: []string{"a", "b"}, expected: []string{"a", "b"}},
		{name: "keeps first occurrence order", input: []string{"b", "a", "b", "c", "a"}, expected: []string{"b", "a", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, Unique(tt.input))
		})
	}
}
