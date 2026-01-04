// Package testhelpers provides testing utilities for the Stackit CLI,
// including a scene system, Git repository helpers, and custom assertions.
package testhelpers

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

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

// ExpectCommits asserts that the repository has the expected commit messages
// on the current branch.
func ExpectCommits(t *testing.T, repo *GitRepo, branch string, expected []string) {
	t.Helper()

	cmd := exec.Command("git", "-C", repo.Dir,
		"log", "--oneline", "--format=%s", branch)
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to list commits")

	commits := strings.Split(strings.TrimSpace(string(output)), "\n")

	// Filter out empty strings
	filtered := []string{}
	for _, c := range commits {
		c = strings.TrimSpace(c)
		if c != "" {
			filtered = append(filtered, c)
		}
	}

	// Compare only the first N commits where N is the length of expected
	if len(filtered) < len(expected) {
		require.Fail(t, "Not enough commits", "Expected %d commits, got %d", len(expected), len(filtered))
		return
	}

	actual := filtered[:len(expected)]
	require.Equal(t, expected, actual, "Commits do not match")
}

// ExpectCommitsString asserts that the repository has the expected commit messages
// as a comma-separated string (matching TypeScript API).
func ExpectCommitsString(t *testing.T, repo *GitRepo, expected string) {
	t.Helper()

	messages, err := repo.ListCurrentBranchCommitMessages()
	require.NoError(t, err, "Failed to list commit messages")

	// Take only the first N commits where N is the number in expected
	expectedCount := len(strings.Split(expected, ","))
	if len(messages) < expectedCount {
		require.Fail(t, "Not enough commits", "Expected %d commits, got %d", expectedCount, len(messages))
		return
	}

	actual := strings.Join(messages[:expectedCount], ", ")
	require.Equal(t, expected, actual, "Commits do not match")
}

// NormalizeOutput removes variable parts of output and extra whitespace for comparison.
// It removes empty lines to make test output comparisons more stable.
func NormalizeOutput(output string) string {
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
