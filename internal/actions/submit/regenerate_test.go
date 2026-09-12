package submit

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type regenerationEngine struct {
	engine.Engine
	writes int
}

func (e *regenerationEngine) BatchGetPRSubmissionStatus(_ context.Context, branches engine.Branches) (map[string]engine.PRSubmissionStatus, error) {
	statuses := make(map[string]engine.PRSubmissionStatus)
	for _, branch := range branches {
		pr, err := branch.GetPrInfo()
		if err != nil {
			return nil, err
		}
		statuses[branch.GetName()] = engine.PRSubmissionStatus{
			Action: engine.SubmitActionUpdate, PRInfo: pr, PRNumber: pr.Number(), NeedsUpdate: false,
		}
	}
	return statuses, nil
}

func (e *regenerationEngine) BatchUpsertPrInfo(ctx context.Context, updates map[string]*engine.PrInfo) error {
	e.writes++
	return e.Engine.BatchUpsertPrInfo(ctx, updates)
}

type regenerationClient struct {
	github.Client
	update github.UpdatePROptions
}

func (c *regenerationClient) UpdatePullRequest(_ context.Context, _ int, opts github.UpdatePROptions) ([]string, error) {
	c.update = opts
	return nil, nil
}

func TestRegeneratePlanningAndEmptyBody(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").CommitChange("file", "feat: replacement").TrackBranch("feature", "main")
	branch := s.Engine.GetBranch("feature")
	old := testhelpers.NewTestPrInfoEmpty().WithNumber(new(42)).WithTitle("Remote title").
		WithBody("Remote description").WithBase("main").WithURL("https://example.com/pr/42").WithIsDraft(true)
	require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), branch, old))
	eng := &regenerationEngine{Engine: s.Engine}
	client := &regenerationClient{}
	s.Context.Engine, s.Context.GitHubClient = eng, client
	branches := engine.BranchesOf(branch)

	ordinary, err := prepareBranchesForSubmit(s.Context, branches, Options{NoEdit: true}, "feature", nil, nil, NewJSONHandler())
	require.NoError(t, err)
	require.Empty(t, ordinary, "unchanged PRs are normally skipped")

	handler := NewJSONHandler()
	preview, err := prepareBranchesForSubmit(s.Context, branches, Options{Regenerate: true, NoEdit: true, DryRun: true}, "feature", nil, nil, handler)
	require.NoError(t, err)
	require.Len(t, preview, 1, "regeneration must include unchanged PRs")
	require.Equal(t, "feat: replacement", preview[0].Metadata.Title)
	require.Empty(t, preview[0].Metadata.Body)
	require.True(t, preview[0].Metadata.IsDraft, "regeneration preserves draft state")
	require.Zero(t, eng.writes, "dry-run must not save replacement text")
	stored, err := branch.GetPrInfo()
	require.NoError(t, err)
	require.Equal(t, old.Title(), stored.Title())
	require.Equal(t, old.Body(), stored.Body())
	require.Equal(t, &PRContentPreview{Title: "feat: replacement", Body: ""}, handler.Result.Branches[0].Regenerated)

	planned, err := prepareBranchesForSubmit(s.Context, branches, Options{Regenerate: true, NoEdit: true}, "feature", nil, nil, NewJSONHandler())
	require.NoError(t, err)
	require.Len(t, planned, 1)
	require.Equal(t, 1, eng.writes)
	_, err = updatePullRequestQuiet(s.Context, planned[0], Options{Regenerate: true}, NewJSONHandler())
	require.NoError(t, err)
	require.NotNil(t, client.update.Body, "an empty regenerated body must clear the old description")
	require.Empty(t, *client.update.Body)
	require.Equal(t, "feat: replacement", *client.update.Title)
	require.Nil(t, client.update.Draft)

	_, err = updatePullRequestQuiet(s.Context, planned[0], Options{}, NewJSONHandler())
	require.NoError(t, err)
	require.Nil(t, client.update.Body, "ordinary updates still omit an empty body")
}

func TestRegenerateUsesCurrentCommitMessages(t *testing.T) {
	t.Parallel()
	for _, multiple := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "multiple"}[multiple], func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
			s.CreateBranch("feature").CommitChange("first", "feat: first\n\nFirst body").TrackBranch("feature", "main")
			if multiple {
				s.CommitChange("second", "fix: second\n\nSecond body")
			}
			branch := s.Engine.GetBranch("feature")
			require.NoError(t, s.Engine.SetScope(context.Background(), branch, engine.NewScope("PROJ-1")))
			old := testhelpers.NewTestPrInfoEmpty().WithNumber(new(42)).WithTitle("Old title").WithBody("Old body")
			require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), branch, old))
			// Any attempted GitHub read would panic: existing remote content is irrelevant.
			s.Context.GitHubClient = &regenerationClient{}
			metadata, err := PreparePRMetadata(branch, MetadataOptions{Regenerate: true, NoEdit: true}, s.Context)
			require.NoError(t, err)
			require.Equal(t, engine.NewScope("PROJ-1").ApplyToTitle("feat: first"), metadata.Title)
			if multiple {
				require.Equal(t, "- feat: first\n- fix: second", metadata.Body)
			} else {
				require.Equal(t, "First body", strings.TrimSpace(metadata.Body))
			}
			preserved, err := PreparePRMetadata(branch, MetadataOptions{NoEdit: true}, s.Context)
			require.NoError(t, err)
			require.Equal(t, old.Title(), preserved.Title)
			require.Equal(t, old.Body(), preserved.Body)
		})
	}
}
