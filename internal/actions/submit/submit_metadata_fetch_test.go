package submit_test

import (
	"context"
	"testing"

	gh "github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/submit"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestPreparePRMetadata_FetchFromGitHub(t *testing.T) {
	t.Run("fetches body from GitHub when local body is empty", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		branchName := featureBranch
		prNumber := 123
		githubBody := "This body exists only on GitHub"

		// 1. Setup branch with PR info but EMPTY body in local metadata
		branch := s.Engine.GetBranch(branchName)
		err := s.Engine.UpsertPrInfo(context.Background(), branch, testhelpers.NewTestPrInfoEmpty().
			WithNumber(&prNumber).
			WithTitle("Existing Title").
			WithBody("")) // Empty locally
		require.NoError(t, err)

		// 2. Setup Mock GitHub Client
		config := testhelpers.NewMockGitHubServerConfig()
		config.PRs[branchName] = &gh.PullRequest{
			Number: new(prNumber),
			Title:  new("Existing Title"),
			Body:   new(githubBody),
			Head:   &gh.PullRequestBranch{Ref: new(branchName)},
			Base:   &gh.PullRequestBranch{Ref: new("main")},
		}
		config.UpdatedPRs[prNumber] = config.PRs[branchName]

		ghClient, _, _ := testhelpers.NewMockGitHubClient(t, config)
		mockClient := testhelpers.NewMockGitHubClientInterface(ghClient, config.Owner, config.Repo, config)
		s.Context.GitHubClient = mockClient

		// 3. Prepare metadata without editing
		opts := submit.MetadataOptions{
			Edit: false,
		}

		metadata, err := submit.PreparePRMetadata(branch, opts, s.Context)
		require.NoError(t, err)

		// 4. Verify body was fetched from GitHub
		require.Equal(t, githubBody, metadata.Body, "Metadata body should have been fetched from GitHub since local was empty")
	})

	t.Run("GetPRBody uses existingBody when not empty", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		branch := s.Engine.GetBranch("main") // just need a branch object

		existing := "Already have this body"
		body, err := submit.GetPRBody(branch, false, existing)
		require.NoError(t, err)
		require.Equal(t, existing, body)
	})
}
