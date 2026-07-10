package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

func TestSelectBranchesForDeletion(t *testing.T) {
	t.Parallel()

	plan := newDeletionPlan()
	plan.add("parent", engine.DeletionStatus{}, map[string]bool{"child": true})
	plan.add("child", engine.DeletionStatus{}, map[string]bool{})

	selected := selectBranchesForDeletion(plan, func(name string) bool {
		return name != "child"
	}, func(name string) string {
		return map[string]string{"child": "parent"}[name]
	})

	require.Empty(t, selected)
	require.Contains(t, plan.branches, "parent")
	require.Contains(t, plan.branches["parent"].blockers, "child")
	require.NotContains(t, plan.branches, "child")
}
