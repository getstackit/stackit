package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
)

func TestNormalizeAPIPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "default",
			input:    nil,
			expected: []string{"/api/v1"},
		},
		{
			name:     "trim and dedupe",
			input:    []string{" api/v1 ", "/api/v1/"},
			expected: []string{"/api/v1"},
		},
		{
			name:     "empty values fallback to default",
			input:    []string{"", "  "},
			expected: []string{"/api/v1"},
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
	prefixes := []string{"/api/v1"}

	tests := []struct {
		path string
		want bool
	}{
		{path: "/api", want: false},
		{path: "/api/stacks", want: false},
		{path: "/api/v1", want: true},
		{path: "/api/v1/view", want: true},
		{path: "/api/v12/view", want: false},
		{path: "/dashboard", want: false},
	}

	for _, tc := range tests {
		if got := isAPIPath(tc.path, prefixes); got != tc.want {
			t.Fatalf("path %q: want %v, got %v", tc.path, tc.want, got)
		}
	}
}

func TestServerShutdownIsIdempotentWithoutHTTPServer(t *testing.T) {
	server := NewServer(ServerConfig{})

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestRegistryCloseClosesEntryBroadcasters(t *testing.T) {
	reg := registry.New()
	b := registry.NewBroadcaster()
	entry := &registry.RepoEntry{
		ID:          "default",
		Broadcaster: b,
	}
	entry.AddCloser(func() error { b.Close(); return nil })
	require.NoError(t, reg.Add(entry))

	require.NoError(t, reg.Close())

	select {
	case <-b.Done():
	case <-time.After(time.Second):
		t.Fatal("broadcaster was not closed after registry.Close")
	}
}
