package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBranches(t *testing.T) {
	t.Parallel()

	main := NewBranch("main", nil)
	feature := NewBranch("feature", nil)
	bugfix := NewBranch("bugfix", nil)

	t.Run("preserves order and removes duplicates", func(t *testing.T) {
		t.Parallel()

		branches := NewBranches([]Branch{main, feature, main, bugfix})

		require.Equal(t, []string{"main", "feature", "bugfix"}, branches.Names())
		require.Equal(t, 3, branches.Len())
		require.True(t, branches.Contains("feature"))
		require.False(t, branches.Contains("missing"))
	})

	t.Run("All returns a copy", func(t *testing.T) {
		t.Parallel()

		branches := NewBranches([]Branch{main, feature})
		all := branches.All()
		all[0] = bugfix

		require.Equal(t, []string{"main", "feature"}, branches.Names())
	})

	t.Run("Append returns a new ordered set", func(t *testing.T) {
		t.Parallel()

		branches := BranchesOf(main).Append(feature, main, bugfix)

		require.Equal(t, []string{"main", "feature", "bugfix"}, branches.Names())
	})

	t.Run("Last and Reverse preserve set semantics", func(t *testing.T) {
		t.Parallel()

		branches := NewBranches([]Branch{main, feature, bugfix})

		require.Equal(t, []string{"feature", "bugfix"}, branches.Last(2).Names())
		require.Equal(t, []string{"bugfix", "feature", "main"}, branches.Reverse().Names())
		require.Empty(t, branches.Last(0).Names())
	})
}

func TestBranchStatuses(t *testing.T) {
	t.Parallel()

	statuses := newBranchStatuses(map[string]bool{
		"main":    true,
		"feature": false,
	})

	require.True(t, statuses.IsUpToDate(NewBranch("main", nil)))
	require.False(t, statuses.IsUpToDate(NewBranch("feature", nil)))
	require.False(t, statuses.IsUpToDateByName("missing"))
}
