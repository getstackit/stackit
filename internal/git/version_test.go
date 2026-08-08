package git

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGitVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		major   int
		minor   int
		wantErr bool
	}{
		{name: "plain", output: "git version 2.39.2", major: 2, minor: 39},
		{name: "windows suffix", output: "git version 2.35.1.windows.2", major: 2, minor: 35},
		{name: "apple suffix", output: "git version 2.39.5 (Apple Git-154)", major: 2, minor: 39},
		{name: "trailing newline", output: "git version 2.36.0\n", major: 2, minor: 36},
		{name: "two-component version", output: "git version 3.0", major: 3, minor: 0},
		{name: "empty", output: "", wantErr: true},
		{name: "unrelated output", output: "command not found", wantErr: true},
		{name: "no minor component", output: "git version 2", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			major, minor, err := parseGitVersion(tt.output)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.major, major)
			require.Equal(t, tt.minor, minor)
		})
	}
}

func TestAtLeast(t *testing.T) {
	t.Parallel()

	require.True(t, AtLeast(2, 36, 2, 36), "equal meets the minimum")
	require.True(t, AtLeast(2, 49, 2, 36), "same major, higher minor")
	require.True(t, AtLeast(3, 0, 2, 36), "higher major with lower minor still passes")
	require.False(t, AtLeast(2, 35, 2, 36), "same major, lower minor")
	require.False(t, AtLeast(1, 99, 2, 36), "lower major with higher minor still fails")
}

// The version gate sits in front of ListWorktrees, which nearly every stack
// command calls before moving a ref. A parse that stops matching the installed
// git's output would take the whole tool down, so pin it against the real one.
func TestGitVersionClearsMinimum(t *testing.T) {
	t.Parallel()

	r := &runner{}
	major, minor, err := r.GitVersion(context.Background())
	require.NoError(t, err)
	require.NoError(t, r.requireGitVersion(context.Background(), MinGitMajor, MinGitMinor, "test"),
		"installed git %d.%d is below the minimum this test suite assumes", major, minor)
}

func TestRequireGitVersionDistinguishesTooOldFromUnknown(t *testing.T) {
	t.Parallel()

	r := &runner{}

	// Too old: name the versions so the user knows what they have.
	err := r.requireGitVersion(context.Background(), 99, 0, "git future-feature")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires Git 99.0 or later")
	require.Contains(t, err.Error(), "please upgrade")

	// Unknown: a probe failure must NOT be reported as an old git. Point the
	// runner at a directory with no git to force the probe to fail.
	broken := &runner{repoRoot: "/nonexistent-stackit-version-probe"}
	brokenErr := broken.requireGitVersion(context.Background(), MinGitMajor, MinGitMinor, "git worktree list -z")
	require.Error(t, brokenErr)
	require.Contains(t, brokenErr.Error(), "could not be determined")
	require.NotContains(t, brokenErr.Error(), "please upgrade")
}

// A failed probe must not be cached: latching it would make every later check
// report a perfectly modern git as too old for the runner's lifetime.
func TestGitVersionDoesNotCacheFailure(t *testing.T) {
	t.Parallel()

	r := &runner{repoRoot: "/nonexistent-stackit-version-probe"}
	_, _, err := r.GitVersion(context.Background())
	require.Error(t, err)
	require.False(t, r.gitVersionParsed, "a failed probe must not be cached")

	// Same runner, now able to reach git: the earlier failure must not persist.
	r.repoRoot = ""
	major, _, err := r.GitVersion(context.Background())
	require.NoError(t, err, "a failed probe must be retried, not latched")
	require.Positive(t, major)
}
