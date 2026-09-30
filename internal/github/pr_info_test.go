package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateGitHubClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		hostname      string
		wantBaseURL   string
		wantUploadURL string
	}{
		{
			name:          "github.com uses default API URLs",
			hostname:      "github.com",
			wantBaseURL:   "https://api.github.com/",
			wantUploadURL: "https://uploads.github.com/",
		},
		{
			name:          "enterprise host uses api/v3 and api/uploads",
			hostname:      "github.company.com",
			wantBaseURL:   "https://github.company.com/api/v3/",
			wantUploadURL: "https://github.company.com/api/uploads/",
		},
		{
			name:          "enterprise simple hostname",
			hostname:      "my-internal-github",
			wantBaseURL:   "https://my-internal-github/api/v3/",
			wantUploadURL: "https://my-internal-github/api/uploads/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := createGitHubClient(t.Context(), tt.hostname, "test-token")
			require.NoError(t, err)
			require.Equal(t, tt.wantBaseURL, client.BaseURL())
			require.Equal(t, tt.wantUploadURL, client.UploadURL())
		})
	}
}
