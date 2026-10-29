package actions

import (
	"context"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
)

// TopologyEngine is the engine surface the linear-stack check reads.
type TopologyEngine interface {
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	AllBranches() engine.Branches
	IsTrunk(branch engine.Branch) bool
}

// Warner receives user-facing warnings. output.Output satisfies it.
type Warner interface {
	Warn(format string, args ...any)
}

// WarnIfLinearStackRestored is the *app.Context form of WarnIfNonLinearRestore.
func WarnIfLinearStackRestored(ctx *app.Context, operation string) {
	linear := ctx.Config != nil && ctx.Config.LinearStacks()
	WarnIfNonLinearRestore(ctx.Context, ctx.Engine, ctx.Output, linear, operation)
}

// WarnIfNonLinearRestore reports when snapshot restoration brings back a
// topology that linear mode would not allow newly-created operations to make.
// linearStacks reflects stack.shape=linear.
func WarnIfNonLinearRestore(ctx context.Context, eng TopologyEngine, out Warner, linearStacks bool, operation string) {
	if !linearStacks || !hasNonLinearStack(ctx, eng) {
		return
	}

	out.Warn("%s restored a non-linear stack. stack.shape=linear will block new forks, but this restored topology remains valid; set stack.shape to tree to make that explicit.", operation)
}

func hasNonLinearStack(ctx context.Context, eng TopologyEngine) bool {
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	return slices.ContainsFunc(eng.AllBranches(), func(branch engine.Branch) bool {
		return !eng.IsTrunk(branch) && len(graph.Children(branch)) > 1
	})
}
