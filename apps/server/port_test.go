package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		start          int
		portExplicit   bool
		env            string
		wantPort       int
		wantErrContain string
	}{
		{
			name:     "no env keeps default",
			start:    8080,
			env:      "",
			wantPort: 8080,
		},
		{
			name:     "env populates default",
			start:    8080,
			env:      "3000",
			wantPort: 3000,
		},
		{
			name:     "explicit flag wins over env",
			start:    9999,
			env:      "3000",
			wantPort: 9999, portExplicit: true,
		},
		{
			name:     "whitespace env is treated as unset",
			start:    8080,
			env:      "   ",
			wantPort: 8080,
		},
		{
			name:           "invalid env returns error",
			start:          8080,
			env:            "not-a-number",
			wantErrContain: "invalid PORT env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			port := tt.start
			err := resolvePort(&port, tt.portExplicit, tt.env)
			if tt.wantErrContain != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrContain)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPort, port)
		})
	}
}
