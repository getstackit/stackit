package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestDiscoverIndependentStacks(t *testing.T) {
	t.Parallel()

	t.Run("returns stacks rooted at direct trunk children", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"stack-a":       "main",
				"stack-a-child": "stack-a",
				"stack-a-leaf":  "stack-a-child",
				"stack-b":       "main",
				"stack-b-child": "stack-b",
			})

		stacks := engine.DiscoverIndependentStacks(s.Engine)

		require.Equal(t, []engine.IndependentStack{
			{
				RootBranch: "stack-a",
				Branches:   []string{"stack-a", "stack-a-child", "stack-a-leaf"},
			},
			{
				RootBranch: "stack-b",
				Branches:   []string{"stack-b", "stack-b-child"},
			},
		}, stacks)
	})

	t.Run("returns no stacks when trunk has no tracked children", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		stacks := engine.DiscoverIndependentStacks(s.Engine)

		require.Empty(t, stacks)
	})

	t.Run("includes branching descendants under the same root", func(t *testing.T) {
		t.Parallel()

		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"root":    "main",
				"child-a": "root",
				"child-b": "root",
			})

		stacks := engine.DiscoverIndependentStacks(s.Engine)

		require.Equal(t, []engine.IndependentStack{
			{
				RootBranch: "root",
				Branches:   []string{"root", "child-a", "child-b"},
			},
		}, stacks)
	})
}
