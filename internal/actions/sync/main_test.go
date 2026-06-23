package sync

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Set a dummy token so tests don't invoke 'gh auth token' and trigger credential prompts.
	if os.Getenv("GITHUB_TOKEN") == "" {
		os.Setenv("GITHUB_TOKEN", "dummy")
	}
	os.Exit(m.Run())
}
