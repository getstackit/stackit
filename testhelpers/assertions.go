// Package testhelpers provides testing utilities for the Stackit CLI,
// including a scene system, Git repository helpers, and custom assertions.
package testhelpers

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Must is a generic helper function that panics if err is not nil,
// otherwise returns the value. This is useful for test setup code
// where errors are not expected and should halt execution immediately.
// Requires Go 1.18+.
func Must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

// ExpectBranches asserts that the repository has the expected branches.
// It filters out scene-related branches (prod, x2) and compares sorted lists.
func ExpectBranches(t *testing.T, repo *GitRepo, expected []string) {
	t.Helper()

	cmd := exec.Command("git", "-C", repo.Dir,
		"for-each-ref", "refs/heads/", "--format=%(refname:short)")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to list branches")

	branches := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Filter out empty strings and scene-related branches
	filtered := []string{}
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b != "" && b != "prod" && b != "x2" {
			filtered = append(filtered, b)
		}
	}

	// Sort both slices for comparison
	slices.Sort(filtered)
	slices.Sort(expected)

	require.Equal(t, expected, filtered, "Branches do not match")
}

// ExpectBranchesString asserts that the repository has the expected branches
// as a comma-separated sorted string (matching TypeScript API).
func ExpectBranchesString(t *testing.T, repo *GitRepo, expected string) {
	t.Helper()

	cmd := exec.Command("git", "-C", repo.Dir,
		"for-each-ref", "refs/heads/", "--format=%(refname:short)")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to list branches")

	branches := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Filter out empty strings and scene-related branches
	filtered := []string{}
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b != "" && b != "prod" && b != "x2" {
			filtered = append(filtered, b)
		}
	}

	// Sort and join
	slices.Sort(filtered)
	actual := strings.Join(filtered, ", ")

	require.Equal(t, expected, actual, "Branches do not match")
}

// NormalizeOutput removes variable parts of output and extra whitespace for comparison.
// It strips ANSI escape codes (lipgloss v2 always generates them) and removes
// empty lines to make test output comparisons more stable.
func NormalizeOutput(output string) string {
	output = ansi.Strip(output)
	lines := strings.Split(output, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
}
