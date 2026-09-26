package sync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// scriptedOrphanHandler answers orphaned-metadata prompts per branch and
// records the order it was asked in.
type scriptedOrphanHandler struct {
	NullHandler
	push     map[string]bool
	cancelOn string
	asked    []string
}

func (h *scriptedOrphanHandler) PromptOrphanedMetadata(info engine.OrphanedMetadataInfo) (bool, error) {
	h.asked = append(h.asked, info.BranchName)
	if info.BranchName == h.cancelOn {
		return false, errors.ErrCanceled
	}
	return h.push[info.BranchName], nil
}

// setupOrphanedBranches tracks each branch on main, marks it as previously
// synced, then edits its metadata so it is orphaned with local changes.
// No remote metadata ref exists, so every branch prompts.
func setupOrphanedBranches(t *testing.T, s *scenario.Scenario, names ...string) {
	t.Helper()
	for _, name := range names {
		s.Checkout("main").
			CreateBranch(name).
			CommitChange(name+"-file", name).
			TrackBranch(name, "main")
	}
	s.Checkout("main")
	require.NoError(t, s.Engine.BatchSetLastModifiedBy(t.Context(), names))
	for _, name := range names {
		require.NoError(t, s.Engine.SetScope(t.Context(), s.Engine.GetBranch(name), engine.NewScope("edited")))
	}
}

func hasLocalOnlyHash(t *testing.T, s *scenario.Scenario, branch string) bool {
	t.Helper()
	blob, err := s.Scene.Repo.RunGitCommandAndGetOutput("cat-file", "-p", "refs/stackit/metadata/"+branch)
	require.NoError(t, err)
	return strings.Contains(blob, "localOnlyHash")
}

func onRemote(t *testing.T, s *scenario.Scenario, branch string) bool {
	t.Helper()
	out, err := s.Scene.Repo.RunGitCommandAndGetOutput("ls-remote", "origin", "refs/stackit/metadata/"+branch)
	require.NoError(t, err)
	return strings.TrimSpace(out) != ""
}

func TestHandleOrphanedMetadata(t *testing.T) {
	t.Parallel()

	t.Run("cancel applies answers already given", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t)
		setupOrphanedBranches(t, s, "a", "b", "c")

		h := &scriptedOrphanHandler{push: map[string]bool{"a": true}, cancelOn: "b"}
		err := handleOrphanedMetadata(s.Context, &Options{}, h)

		require.ErrorIs(t, err, errors.ErrCanceled)
		require.Equal(t, []string{"a", "b"}, h.asked, "prompts stop at the cancel")
		require.True(t, onRemote(t, s, "a"), "push answered before the cancel is applied")
		require.False(t, onRemote(t, s, "b"))
		require.False(t, onRemote(t, s, "c"))
		require.True(t, hasLocalOnlyHash(t, s, "b"), "canceled branch keeps its sync state")
		require.True(t, hasLocalOnlyHash(t, s, "c"), "unasked branch keeps its sync state")
	})

	t.Run("push and decline answers are routed separately", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t)
		setupOrphanedBranches(t, s, "a", "b")

		h := &scriptedOrphanHandler{push: map[string]bool{"a": true}}
		require.NoError(t, handleOrphanedMetadata(s.Context, &Options{}, h))

		require.True(t, onRemote(t, s, "a"))
		require.True(t, hasLocalOnlyHash(t, s, "a"), "pushed branch is recorded as synced")
		require.False(t, onRemote(t, s, "b"))
		require.False(t, hasLocalOnlyHash(t, s, "b"), "declined branch accepts the deletion")
	})

	t.Run("push without remote metadata support keeps sync state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		setupOrphanedBranches(t, s, "a")

		h := &scriptedOrphanHandler{push: map[string]bool{"a": true}}
		require.NoError(t, handleOrphanedMetadata(s.Context, &Options{}, h))

		require.True(t, hasLocalOnlyHash(t, s, "a"), "unpushed branch stays orphaned so the next sync asks again")
	})
}
