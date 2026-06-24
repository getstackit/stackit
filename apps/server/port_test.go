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
		prod           bool
		env            string
		wantPort       int
		wantErrContain string
	}{
		{
			name:     "prod no env keeps default",
			start:    8080,
			prod:     true,
			env:      "",
			wantPort: 8080,
		},
		{
			name:     "prod env populates default",
			start:    8080,
			prod:     true,
			env:      "3000",
			wantPort: 3000,
		},
		{
			name:     "explicit flag wins over env",
			start:    9999,
			prod:     true,
			env:      "3000",
			wantPort: 9999, portExplicit: true,
		},
		{
			name:     "prod whitespace env is treated as unset",
			start:    8080,
			prod:     true,
			env:      "   ",
			wantPort: 8080,
		},
		{
			name:           "prod invalid env returns error",
			start:          8080,
			prod:           true,
			env:            "not-a-number",
			wantErrContain: "invalid PORT env",
		},
		{
			name:     "local ignores env",
			start:    8080,
			prod:     false,
			env:      "3000",
			wantPort: 8080,
		},
		{
			name:     "local ignores invalid env without error",
			start:    8080,
			prod:     false,
			env:      "not-a-number",
			wantPort: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			port := tt.start
			err := resolvePort(&port, tt.portExplicit, tt.prod, tt.env)
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
