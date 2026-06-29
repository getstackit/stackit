package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

// findDecoration returns the decoration with the given name across all SHAs,
// along with the SHA it points at.
func findDecoration(decos map[string][]git.RefDecoration, name string) (string, git.RefDecoration, bool) {
	for sha, list := range decos {
		for _, d := range list {
			if d.Name == name {
				return sha, d, true
			}
		}
	}
	return "", git.RefDecoration{}, false
}

func TestRefDecorations(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// A second commit so HEAD and the tag below can point at distinct commits.
	require.NoError(t, scene.Repo.CreateChange("file1", "content1", false))
	require.NoError(t, scene.Repo.RunGitCommand("add", "."))
	require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", "second"))

	// Lightweight tag and annotated tag on the current commit.
	require.NoError(t, scene.Repo.RunGitCommand("tag", "v1.0.0"))
	require.NoError(t, scene.Repo.RunGitCommand("tag", "-a", "v2.0.0", "-m", "release 2"))
	// A separate branch head.
	require.NoError(t, scene.Repo.RunGitCommand("branch", "feature"))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	decos, err := runner.RefDecorations()
	require.NoError(t, err)

	headSHA, err := runner.GetRef("refs/heads/main")
	require.NoError(t, err)

	// main and feature both point at the tip commit, as branch decorations.
	mainSHA, mainDeco, ok := findDecoration(decos, "main")
	require.True(t, ok, "expected main decoration")
	require.False(t, mainDeco.IsTag)
	require.Equal(t, headSHA, mainSHA)

	_, featureDeco, ok := findDecoration(decos, "feature")
	require.True(t, ok, "expected feature decoration")
	require.False(t, featureDeco.IsTag)

	// Lightweight tag points straight at the commit.
	lwSHA, lwDeco, ok := findDecoration(decos, "v1.0.0")
	require.True(t, ok, "expected lightweight tag decoration")
	require.True(t, lwDeco.IsTag)
	require.Equal(t, headSHA, lwSHA)

	// Annotated tag is dereferenced to the wrapped commit, not the tag object.
	annSHA, annDeco, ok := findDecoration(decos, "v2.0.0")
	require.True(t, ok, "expected annotated tag decoration")
	require.True(t, annDeco.IsTag)
	require.Equal(t, headSHA, annSHA, "annotated tag should dereference to the commit SHA")

	// All four refs point at HEAD: branch heads come first (alphabetical), then
	// tags (alphabetical) — git-log-style, not for-each-ref's refname order.
	require.Equal(t, []git.RefDecoration{
		{Name: "feature"},
		{Name: "main"},
		{Name: "v1.0.0", IsTag: true},
		{Name: "v2.0.0", IsTag: true},
	}, decos[headSHA])
}
