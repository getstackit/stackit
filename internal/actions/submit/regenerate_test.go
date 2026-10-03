package submit

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type regenerationEngine struct {
	engine.Engine
	writes int
}

func (e *regenerationEngine) BatchGetPRSubmissionStatus(_ context.Context, branches engine.Branches, _ engine.BranchRemoteStatuses) (map[string]engine.PRSubmissionStatus, error) {
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

func (c *regenerationClient) UpdatePullRequest(_ context.Context, _ git.PRNumber, opts github.UpdatePROptions) ([]string, error) {
	c.update = opts
	return nil, nil
}

func TestRegeneratePlanningAndEmptyBody(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").CommitChange("file", "feat: replacement").TrackBranch("feature", "main")
	branch := s.Engine.GetBranch("feature")
	old := testhelpers.NewTestPrInfoEmpty().WithNumber(new(git.PRNumber(42))).WithTitle("Remote title").
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
	preview, err := prepareBranchesForSubmit(s.Context, branches, Options{Text: PRTextRegenerate, NoEdit: true, DryRun: true}, "feature", nil, nil, handler)
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

	confirmHandler := NewJSONHandler()
	_, err = prepareBranchesForSubmit(s.Context, branches, Options{Text: PRTextRegenerate, NoEdit: true, Confirm: true}, "feature", nil, nil, confirmHandler)
	require.NoError(t, err)
	require.NotNil(t, confirmHandler.Result.Branches[0].Regenerated, "--confirm must show the replacement text it asks about")

	planned, err := prepareBranchesForSubmit(s.Context, branches, Options{Text: PRTextRegenerate, NoEdit: true}, "feature", nil, nil, NewJSONHandler())
	require.NoError(t, err)
	require.Len(t, planned, 1)
	require.Equal(t, 2, eng.writes, "both non-dry-run plans save the prepared text")
	_, err = updatePullRequestQuiet(s.Context, planned[0], Options{Text: PRTextRegenerate}, NewJSONHandler())
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
	for _, tt := range []struct {
		name     string
		multiple bool
	}{{name: "single", multiple: false}, {name: "multiple", multiple: true}} {
		multiple := tt.multiple
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
			s.CreateBranch("feature").CommitChange("first", "feat: first\n\nFirst body").TrackBranch("feature", "main")
			if multiple {
				s.CommitChange("second", "fix: second\n\nSecond body")
			}
			branch := s.Engine.GetBranch("feature")
			require.NoError(t, s.Engine.SetScope(context.Background(), branch, engine.NewScope("PROJ-1")))
			old := testhelpers.NewTestPrInfoEmpty().WithNumber(new(git.PRNumber(42))).WithTitle("Old title").WithBody("Old body")
			require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), branch, old))
			// Any attempted GitHub read would panic: existing remote content is irrelevant.
			s.Context.GitHubClient = &regenerationClient{}
			metadata, err := PreparePRMetadata(branch, MetadataOptions{Text: PRTextRegenerate}, s.Context, nil)
			require.NoError(t, err)
			require.Equal(t, engine.NewScope("PROJ-1").ApplyToTitle("feat: first"), metadata.Title)
			if multiple {
				require.Equal(t, "- feat: first\n- fix: second", metadata.Body)
			} else {
				require.Equal(t, "First body", strings.TrimSpace(metadata.Body))
			}
			preserved, err := PreparePRMetadata(branch, MetadataOptions{}, s.Context, nil)
			require.NoError(t, err)
			require.Equal(t, old.Title(), preserved.Title)
			require.Equal(t, old.Body(), preserved.Body)
		})
	}
}

type recordingPrompter struct {
	handler.NonInteractivePrompter
	defaultTitle string
}

func (p *recordingPrompter) TextInput(_, defaultValue string) (string, error) {
	p.defaultTitle = defaultValue
	return "edited title", nil
}

func TestRegenerateEditTitlePrefillsRegeneratedText(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").CommitChange("file", "feat: replacement").TrackBranch("feature", "main")
	branch := s.Engine.GetBranch("feature")
	old := testhelpers.NewTestPrInfoEmpty().WithNumber(new(git.PRNumber(42))).WithTitle("Old title").WithBody("Old body")
	require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), branch, old))
	prompter := &recordingPrompter{}
	s.Context.GitHubClient, s.Context.Prompter = &regenerationClient{}, prompter

	metadata, err := PreparePRMetadata(branch, MetadataOptions{Text: PRTextRegenerate, EditTitle: true}, s.Context, nil)
	require.NoError(t, err)
	require.Equal(t, "feat: replacement", prompter.defaultTitle, "the title prompt must start from regenerated text, not the old title")
	require.Equal(t, "edited title", metadata.Title)
	require.NotEqual(t, old.Body(), metadata.Body, "the unedited body is regenerated too")
}

func TestRegenerateKeepsLockBanner(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").CommitChange("file", "feat: replacement\n\nNew body").TrackBranch("feature", "main")
	branch := s.Engine.GetBranch("feature")
	_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
	require.NoError(t, err)

	for _, footer := range []bool{false, true} {
		metadata := &PRMetadata{Title: "feat: replacement", Body: "New body"}
		composeRegeneratedBodies(s.Context, Options{Text: PRTextRegenerate, SubmitFooter: footer}, []Info{
			{BranchName: "feature", Action: engine.SubmitActionUpdate, Metadata: metadata},
		})
		// The footer pass may not run (submit.footer=false), so the body sent
		// must itself carry the lock banner.
		require.True(t, strings.HasPrefix(metadata.Body, pr.LockSectionStart), "footer=%v: lock banner leads the body: %q", footer, metadata.Body)
		require.Contains(t, metadata.Body, "New body")
	}
}

func TestOverlaySentContentPrefersWrittenText(t *testing.T) {
	t.Parallel()
	updated, created := git.PRNumber(1), git.PRNumber(2)
	current := map[git.PRNumber]github.PRContent{
		updated: {Title: "Stale title", Body: "Stale body"},
		created: {Title: "Created title", Body: "Created body"},
	}
	overlaySentContent(current, []Info{
		{Action: engine.SubmitActionUpdate, PRNumber: &updated, Metadata: &PRMetadata{Title: "New title", Body: "New body"}},
		{Action: engine.SubmitActionCreate, PRNumber: &created, Metadata: &PRMetadata{Title: "Ignored", Body: "Ignored"}},
	})
	require.Equal(t, github.PRContent{Title: "New title", Body: "New body"}, current[updated])
	require.Equal(t, "Created title", current[created].Title, "creates already read back what they wrote")
}

func TestOptionsResolveEditFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		opts                Options
		wantTitle, wantBody bool
	}{
		{name: "no flags edits nothing", opts: Options{}},
		{name: "--edit edits both", opts: Options{Edit: true}, wantTitle: true, wantBody: true},
		{name: "--edit --no-edit edits nothing", opts: Options{Edit: true, NoEdit: true}},
		{name: "--edit --no-edit-title edits only the body", opts: Options{Edit: true, NoEditTitle: true}, wantBody: true},
		{name: "--edit --no-edit-description edits only the title", opts: Options{Edit: true, NoEditDescription: true}, wantTitle: true},
		{name: "--edit-title alone", opts: Options{EditTitle: true}, wantTitle: true},
		{name: "--no-edit does not cancel a specific --edit-title", opts: Options{NoEdit: true, EditTitle: true}, wantTitle: true},
		{name: "--edit-description --no-edit-description cancels", opts: Options{EditDescription: true, NoEditDescription: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantTitle, tt.opts.editTitle(), "title")
			require.Equal(t, tt.wantBody, tt.opts.editBody(), "body")
		})
	}
}
