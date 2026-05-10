package merge

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestDetermineRecommendedStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		branchCount int
		want        Strategy
	}{
		{
			name:        "zero branches returns bottom-up",
			branchCount: 0,
			want:        StrategyBottomUp,
		},
		{
			name:        "one branch returns bottom-up",
			branchCount: 1,
			want:        StrategyBottomUp,
		},
		{
			name:        "two branches returns bottom-up",
			branchCount: 2,
			want:        StrategyBottomUp,
		},
		{
			name:        "three branches returns ship",
			branchCount: 3,
			want:        StrategyShip,
		},
		{
			name:        "many branches returns ship",
			branchCount: 10,
			want:        StrategyShip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetermineRecommendedStrategy(tt.branchCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetAvailableScopes(t *testing.T) {
	t.Parallel()

	t.Run("no tracked branches returns empty", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		scopes := GetAvailableScopes(s.Engine)

		require.Empty(t, scopes)
	})

	t.Run("branches without scopes returns empty", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3() // Creates a, b, c without scopes

		scopes := GetAvailableScopes(s.Engine)

		require.Empty(t, scopes)
	})

	t.Run("single scope returns it", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		branchA := s.Engine.GetBranch("a")
		require.NoError(t, s.Engine.SetScope(context.Background(), branchA, engine.NewScope("PROJ-100")))

		scopes := GetAvailableScopes(s.Engine)

		require.Len(t, scopes, 1)
		require.Equal(t, "PROJ-100", scopes[0])
	})

	t.Run("multiple branches same scope deduplicates", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		ctx := context.Background()
		branchA := s.Engine.GetBranch("a")
		branchB := s.Engine.GetBranch("b")
		require.NoError(t, s.Engine.SetScope(ctx, branchA, engine.NewScope("PROJ-100")))
		require.NoError(t, s.Engine.SetScope(ctx, branchB, engine.NewScope("PROJ-100")))

		scopes := GetAvailableScopes(s.Engine)

		require.Len(t, scopes, 1)
		require.Equal(t, "PROJ-100", scopes[0])
	})

	t.Run("mixed scopes with none and clear filtered out", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		ctx := context.Background()
		branchA := s.Engine.GetBranch("a")
		branchB := s.Engine.GetBranch("b")
		branchC := s.Engine.GetBranch("c")
		require.NoError(t, s.Engine.SetScope(ctx, branchA, engine.NewScope("PROJ-100")))
		require.NoError(t, s.Engine.SetScope(ctx, branchB, engine.None()))
		require.NoError(t, s.Engine.SetScope(ctx, branchC, engine.NewScope("PROJ-200")))

		scopes := GetAvailableScopes(s.Engine)
		sort.Strings(scopes)

		require.Len(t, scopes, 2)
		require.Equal(t, []string{"PROJ-100", "PROJ-200"}, scopes)
	})
}

func TestAnalyzeMidStackScope(t *testing.T) {
	t.Parallel()

	t.Run("empty scope returns nil", func(t *testing.T) {
		t.Parallel()
		plan := &Plan{UpstackBranches: []string{"branch1"}}
		got := AnalyzeMidStackScope(nil, plan, "")
		require.Nil(t, got)
	})

	t.Run("no upstack branches returns nil", func(t *testing.T) {
		t.Parallel()
		plan := &Plan{UpstackBranches: []string{}}
		got := AnalyzeMidStackScope(nil, plan, "feature")
		require.Nil(t, got)
	})

	t.Run("upstack branch with matching scope is returned", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		ctx := context.Background()
		branchB := s.Engine.GetBranch("b")
		branchC := s.Engine.GetBranch("c")
		require.NoError(t, s.Engine.SetScope(ctx, branchB, engine.NewScope("PROJ-100")))
		require.NoError(t, s.Engine.SetScope(ctx, branchC, engine.NewScope("PROJ-100")))

		plan := &Plan{UpstackBranches: []string{"b", "c"}}
		got := AnalyzeMidStackScope(s.Engine, plan, "PROJ-100")

		require.Equal(t, []string{"b", "c"}, got)
	})

	t.Run("upstack branch with different scope is not returned", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		ctx := context.Background()
		branchB := s.Engine.GetBranch("b")
		branchC := s.Engine.GetBranch("c")
		require.NoError(t, s.Engine.SetScope(ctx, branchB, engine.NewScope("PROJ-200")))
		require.NoError(t, s.Engine.SetScope(ctx, branchC, engine.NewScope("PROJ-300")))

		plan := &Plan{UpstackBranches: []string{"b", "c"}}
		got := AnalyzeMidStackScope(s.Engine, plan, "PROJ-100")

		require.Nil(t, got)
	})

	t.Run("mix of matching and non-matching scopes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		ctx := context.Background()
		branchB := s.Engine.GetBranch("b")
		branchC := s.Engine.GetBranch("c")
		require.NoError(t, s.Engine.SetScope(ctx, branchB, engine.NewScope("PROJ-100")))
		require.NoError(t, s.Engine.SetScope(ctx, branchC, engine.NewScope("PROJ-200")))

		plan := &Plan{UpstackBranches: []string{"b", "c"}}
		got := AnalyzeMidStackScope(s.Engine, plan, "PROJ-100")

		require.Equal(t, []string{"b"}, got)
	})
}

func TestPostMergeActionRequired_Error(t *testing.T) {
	t.Parallel()

	err := &PostMergeActionRequired{Action: PostMergeSyncTrunk}
	assert.Equal(t, "post-merge action required: sync-trunk", err.Error())

	err2 := &PostMergeActionRequired{Action: PostMergeDone}
	assert.Equal(t, "post-merge action required: done", err2.Error())
}
