package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOptionsCoversAllKeys verifies that all Key* constants from keys.go
// are represented in Options. This ensures the template stays complete.
func TestOptionsCoversAllKeys(t *testing.T) {
	t.Parallel()

	// All keys from keys.go that should be in Options
	allKeys := []string{
		KeyTrunk,
		KeyTrunks,
		KeyBranchPattern,
		KeySubmitFooter,
		KeyUndoDepth,
		KeyWorktreeBasePath,
		KeyWorktreeAutoClean,
		KeyMergeMethod,
		KeyCICommand,
		KeyCITimeout,
		KeySplitHunkSelector,
		KeyApprovedHooks,
		KeyMaxConcurrency,
		KeyNavigationWhen,
		KeyNavigationMarker,
		KeyNavigationLocation,
		KeyNavigationShowMerged,
		KeySubmitDraft,
		KeyGitHubStack,
		KeySubmitWeb,
		KeySubmitLabels,
		KeySubmitReviewers,
		KeySubmitAssignees,
	}

	// Build set of GitKeys from Options
	coveredKeys := make(map[string]bool)
	for _, opt := range Options {
		coveredKeys[opt.GitKey] = true
	}

	// Verify all keys are covered
	for _, key := range allKeys {
		if !coveredKeys[key] {
			t.Errorf("Config key %q not covered in Options", key)
		}
	}
}

// TestOptionsHaveValidGitKeys ensures all Options have a valid GitKey
// that matches the expected pattern.
func TestOptionsHaveValidGitKeys(t *testing.T) {
	t.Parallel()

	for _, opt := range Options {
		t.Run(opt.YAMLPath, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, opt.GitKey, "Option %q must have a GitKey", opt.YAMLPath)
			require.True(t, strings.HasPrefix(opt.GitKey, "stackit."),
				"Option %q GitKey %q must start with 'stackit.'", opt.YAMLPath, opt.GitKey)
		})
	}
}

// TestOptionsHaveDescriptions ensures all Options have descriptions.
func TestOptionsHaveDescriptions(t *testing.T) {
	t.Parallel()

	for _, opt := range Options {
		t.Run(opt.YAMLPath, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, opt.Description,
				"Option %q must have a Description", opt.YAMLPath)
		})
	}
}

// TestOptionsHaveYAMLPaths ensures all Options have valid YAML paths.
func TestOptionsHaveYAMLPaths(t *testing.T) {
	t.Parallel()

	for _, opt := range Options {
		require.NotEmpty(t, opt.YAMLPath, "Option with GitKey %q must have a YAMLPath", opt.GitKey)
		require.NotEmpty(t, opt.Section, "Option %q must have a Section", opt.YAMLPath)
	}
}

// TestOptionsHaveUniqueGitKeys ensures no two Options share a git key.
func TestOptionsHaveUniqueGitKeys(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, opt := range Options {
		require.False(t, seen[opt.GitKey], "Duplicate git key: %s", opt.GitKey)
		seen[opt.GitKey] = true
	}
}
