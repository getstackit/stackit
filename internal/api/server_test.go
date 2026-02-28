package api

import "testing"

func TestNormalizeAPIPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "default",
			input:    nil,
			expected: []string{"/api/v1", "/api"},
		},
		{
			name:     "trim and dedupe",
			input:    []string{" api/v1 ", "/api/v1/", "/api"},
			expected: []string{"/api/v1", "/api"},
		},
		{
			name:     "empty values fallback to default",
			input:    []string{"", "  "},
			expected: []string{"/api/v1", "/api"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAPIPrefixes(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("want %d entries, got %d (%v)", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("want %v, got %v", tc.expected, got)
				}
			}
		})
	}
}

func TestIsAPIPath(t *testing.T) {
	prefixes := []string{"/api/v1", "/api"}

	tests := []struct {
		path string
		want bool
	}{
		{path: "/api", want: true},
		{path: "/api/stacks", want: true},
		{path: "/api/v1", want: true},
		{path: "/api/v1/view", want: true},
		{path: "/api/v12/view", want: true},
		{path: "/dashboard", want: false},
	}

	for _, tc := range tests {
		if got := isAPIPath(tc.path, prefixes); got != tc.want {
			t.Fatalf("path %q: want %v, got %v", tc.path, tc.want, got)
		}
	}
}
