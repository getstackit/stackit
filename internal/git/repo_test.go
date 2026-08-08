package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestGetRepoInfo(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want git.RemoteRepository
	}{
		{"parses SCP-style SSH URL", "git@github.com:myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses ssh:// protocol SSH URL", "ssh://git@github.com/myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses ssh:// protocol SSH URL without .git suffix", "ssh://git@github.com/myowner/myrepo", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses ssh:// protocol with GitHub Enterprise", "ssh://git@github.enterprise.com/org/project.git", git.RemoteRepository{Host: "github.enterprise.com", Owner: "org", Name: "project"}},
		{"parses HTTPS URL", "https://github.com/myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses HTTPS URL with authentication", "https://user@github.com/myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses ssh:// URL with explicit port", "ssh://git@github.com:22/myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
		{"parses git:// protocol URL", "git://github.com/myowner/myrepo.git", git.RemoteRepository{Host: "github.com", Owner: "myowner", Name: "myrepo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
			require.NoError(t, scene.Repo.RunGitCommand("config", "remote.origin.url", tt.url))

			got, err := git.NewRunnerWithPath(scene.Dir, nil).GetRepoInfo(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("returns zero value for missing remote", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		got, err := git.NewRunnerWithPath(scene.Dir, nil).GetRepoInfo(context.Background())
		require.NoError(t, err)
		require.False(t, got.IsHosted())
	})
}
