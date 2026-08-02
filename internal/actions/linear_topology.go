package actions

import (
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
)

// WarnIfLinearStackRestored reports when snapshot restoration brings back a
// topology that linear mode would not allow newly-created operations to make.
func WarnIfLinearStackRestored(ctx *app.Context, operation string) {
	if ctx.Config == nil || ctx.Config.StackShape() != config.StackShapeLinear || !hasNonLinearStack(ctx.Engine) {
		return
	}

	ctx.Output.Warn("%s restored a non-linear stack. stack.shape=linear will block new forks, but this restored topology remains valid; set stack.shape to tree to make that explicit.", operation)
}

func hasNonLinearStack(eng engine.Engine) bool {
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	for _, branch := range eng.AllBranches() {
		if !eng.IsTrunk(branch) && len(graph.Children(branch)) > 1 {
			return true
		}
	}
	return false
}
