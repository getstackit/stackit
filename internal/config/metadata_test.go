package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
		KeySubmitGitHubStack,
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

// TestGetOptionByGitKey tests lookup by git key.
func TestGetOptionByGitKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		gitKey       string
		expectedPath string
		shouldExist  bool
	}{
		{KeyTrunk, "trunk", true},
		{KeySubmitFooter, "submit.footer", true},
		{KeyBranchPattern, "branch.pattern", true},
		{"stackit.nonexistent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.gitKey, func(t *testing.T) {
			t.Parallel()

			opt := GetOptionByGitKey(tt.gitKey)
			if tt.shouldExist {
				require.NotNil(t, opt, "Expected to find Option for %q", tt.gitKey)
				assert.Equal(t, tt.expectedPath, opt.YAMLPath)
			} else {
				assert.Nil(t, opt)
			}
		})
	}
}

// TestGetOptionByYAMLPath tests lookup by YAML path.
func TestGetOptionByYAMLPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		yamlPath    string
		expectedKey string
		shouldExist bool
	}{
		{"trunk", KeyTrunk, true},
		{"submit.footer", KeySubmitFooter, true},
		{"branch.pattern", KeyBranchPattern, true},
		{"nonexistent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.yamlPath, func(t *testing.T) {
			t.Parallel()

			opt := GetOptionByYAMLPath(tt.yamlPath)
			if tt.shouldExist {
				require.NotNil(t, opt, "Expected to find Option for %q", tt.yamlPath)
				assert.Equal(t, tt.expectedKey, opt.GitKey)
			} else {
				assert.Nil(t, opt)
			}
		})
	}
}

// TestAllGitKeys ensures AllGitKeys returns the expected number of keys.
func TestAllGitKeys(t *testing.T) {
	t.Parallel()

	keys := AllGitKeys()
	require.Equal(t, len(Options), len(keys))

	// Verify all keys are unique
	seen := make(map[string]bool)
	for _, key := range keys {
		require.False(t, seen[key], "Duplicate git key: %s", key)
		seen[key] = true
	}
}
