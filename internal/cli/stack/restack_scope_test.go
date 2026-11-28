package stack

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestackScopeLabel(t *testing.T) {
	t.Parallel()
	const trunk = "main"
	tests := []struct {
		name  string
		scope RestackScope
		want  string
	}{
		{name: "current stack", scope: RestackScope{Target: "feat/api", Trunk: trunk}, want: "current stack"},
		{name: "on trunk restacks every stack", scope: RestackScope{Target: trunk, Trunk: trunk}, want: "all stacks"},
		{name: "--branch main restacks every stack", scope: RestackScope{Target: trunk, Trunk: trunk, ExplicitBranch: true}, want: "all stacks"},
		{name: "--branch names its stack", scope: RestackScope{Target: "feat/api", Trunk: trunk, ExplicitBranch: true}, want: "stack containing feat/api"},
		{name: "--upstack", scope: RestackScope{Target: "feat/api", Trunk: trunk, Upstack: true}, want: "upstack from feat/api"},
		{name: "--upstack from trunk keeps its wording", scope: RestackScope{Target: trunk, Trunk: trunk, Upstack: true}, want: "upstack from main"},
		{name: "--downstack", scope: RestackScope{Target: "feat/api", Trunk: trunk, Downstack: true}, want: "downstack from feat/api"},
		{name: "--only", scope: RestackScope{Target: "feat/api", Trunk: trunk, Only: true}, want: "feat/api"},
		{name: "--all-stacks", scope: RestackScope{Trunk: trunk, AllStacks: true}, want: "all stacks"},
		{name: "--stacks with one root", scope: RestackScope{Trunk: trunk, Stacks: []string{"feat/api"}}, want: "stack feat/api"},
		{name: "--stacks with several roots", scope: RestackScope{Trunk: trunk, Stacks: []string{"feat/api", "feat/web"}}, want: "stacks feat/api, feat/web"},
		{name: "timestamped names are shortened", scope: RestackScope{Target: "jonnii/20260912123107/api", Trunk: trunk, Only: true}, want: "api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.scope.Label())
		})
	}
}

func TestRestackHeadline(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Restacking all stacks · 1 branch", RestackHeadline(RestackScope{AllStacks: true}, 1))
	require.Equal(t, "Restacking current stack · 3 branches", RestackHeadline(RestackScope{Target: "feat/api", Trunk: "main"}, 3))
}
