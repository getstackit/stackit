package merge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/pr"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPRContentGenerator_GenerateConsolidationPR(t *testing.T) {
	t.Parallel()
	t.Run("single_branch_no_pr", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 0},
		})

		assert.Equal(t, "Merging 1 PRs", content.Title)
		assert.NotContains(t, content.Body, "#0")
	})

	t.Run("multiple_branches_with_prs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a":   "main",
			"feature-a-1": "feature-a",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 1},
			{BranchName: "feature-a-1", PRNumber: 2},
		})

		assert.Equal(t, "Merging 2 PRs", content.Title)
		assert.Contains(t, content.Body, "#1")
		assert.Contains(t, content.Body, "#2")
	})

	t.Run("branches_with_scope", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})
		_ = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("feature-a"), engine.NewScope("PROJ-123"))

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 1},
		})

		assert.Equal(t, "Merging PROJ-123", content.Title)
		assert.Contains(t, content.Body, "#1")
	})
}

func TestPRContentGenerator_GenerateConsolidationPR_WithStackDescription(t *testing.T) {
	t.Parallel()
	t.Run("with_stack_description_uses_title", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})

		// Set stack description
		_ = s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("feature-a"), &git.StackDescription{
			Title:       "Auth Feature",
			Description: "Implements authentication",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 1},
		})

		assert.Equal(t, "Auth Feature", content.Title)
		assert.Contains(t, content.Body, "**Auth Feature**")
		assert.Contains(t, content.Body, "Implements authentication")
	})

	t.Run("with_stack_description_title_only", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a":   "main",
			"feature-a-1": "feature-a",
		})

		// Set stack description with title only
		_ = s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("feature-a"), &git.StackDescription{
			Title: "My Feature Stack",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 1},
			{BranchName: "feature-a-1", PRNumber: 2},
		})

		assert.Equal(t, "My Feature Stack", content.Title)
		assert.Contains(t, content.Body, "**My Feature Stack**")
	})

	t.Run("no_description_fallback", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})

		// No stack description set

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateConsolidationPR([]BranchMergeInfo{
			{BranchName: "feature-a", PRNumber: 1},
		})

		// Should fall back to default title
		assert.Equal(t, "Merging 1 PRs", content.Title)
		// Body should not contain description formatting
		assert.NotContains(t, content.Body, "**Auth")
	})
}

func TestPRContentGenerator_GenerateMultiStackPR(t *testing.T) {
	t.Parallel()
	t.Run("single_stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateMultiStackPR(
			[]MultiStackInfo{{RootBranch: "feature-a", AllBranches: []string{"feature-a"}}},
			nil,
		)

		assert.Equal(t, "Merging 1 PRs", content.Title)
		assert.Contains(t, content.Body, "feature-a")
	})

	t.Run("with_excluded_stacks", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
		})

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateMultiStackPR(
			[]MultiStackInfo{{RootBranch: "feature-a", AllBranches: []string{"feature-a"}}},
			[]MultiStackExcluded{{Stack: MultiStackInfo{RootBranch: "feature-b"}, Reason: "conflict"}},
		)

		assert.Contains(t, content.Body, "### Excluded")
		assert.Contains(t, content.Body, "feature-b")
		assert.Contains(t, content.Body, "conflict")
	})

	t.Run("mixed_scopes_omits_scope_trailer", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithStack(map[string]string{
			"feature-a": "main",
			"feature-b": "main",
		})

		err := s.Engine.SetScope(context.Background(), s.Engine.GetBranch("feature-a"), engine.NewScope("PROJ-1"))
		assert.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("feature-b"), engine.NewScope("PROJ-2"))
		assert.NoError(t, err)

		gen := NewPRContentGenerator(s.Engine)
		content := gen.GenerateMultiStackPR(
			[]MultiStackInfo{
				{RootBranch: "feature-a", AllBranches: []string{"feature-a"}},
				{RootBranch: "feature-b", AllBranches: []string{"feature-b"}},
			},
			nil,
		)

		assert.Equal(t, "Merging PROJ-1, PROJ-2", content.Title)
		assert.NotContains(t, content.Body, "Stackit-Scope:")
	})
}

func TestResolveUnifiedScope(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when all scopes are empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", pr.ResolveUnifiedScope([]string{"", ""}))
	})

	t.Run("returns the shared scope", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "PROJ-1", pr.ResolveUnifiedScope([]string{"", "PROJ-1", "PROJ-1"}))
	})

	t.Run("returns empty when scopes differ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", pr.ResolveUnifiedScope([]string{"PROJ-1", "PROJ-2"}))
	})
}
