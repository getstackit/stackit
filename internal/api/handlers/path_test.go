package handlers

import "testing"

func TestParseResourcePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		resource  string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "legacy stacks root",
			path:      "/api/stacks",
			resource:  "stacks",
			wantValue: "",
			wantOK:    true,
		},
		{
			name:      "legacy stacks with branch",
			path:      "/api/stacks/main",
			resource:  "stacks",
			wantValue: "main",
			wantOK:    true,
		},
		{
			name:      "versioned stacks with nested branch",
			path:      "/api/v1/stacks/feature/work",
			resource:  "stacks",
			wantValue: "feature/work",
			wantOK:    true,
		},
		{
			name:      "versioned branches with nested branch",
			path:      "/api/v1/branches/jonnii/20260228/feature",
			resource:  "branches",
			wantValue: "jonnii/20260228/feature",
			wantOK:    true,
		},
		{
			name:      "resource not found",
			path:      "/api/v1/view",
			resource:  "stacks",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "resource segment must match boundary",
			path:      "/api/v1/my-stacks/demo",
			resource:  "stacks",
			wantValue: "",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotValue, gotOK := parseResourcePath(tc.path, tc.resource)
			if gotOK != tc.wantOK {
				t.Fatalf("want ok=%v, got %v", tc.wantOK, gotOK)
			}
			if gotValue != tc.wantValue {
				t.Fatalf("want value=%q, got %q", tc.wantValue, gotValue)
			}
		})
	}
}
