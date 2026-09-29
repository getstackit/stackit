package stack

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers/golden"
)

// Golden coverage for the human-readable `sync --dry-run` preview. The dry-run
// is a read-only query, so its renderer takes a plain sync.DryRunPlan — no engine, no
// repository. See internal/actions/sync/plan.go:PlanDryRun for where the plan is built.

type syncDryRunGoldenCase struct {
	name string
	plan sync.DryRunPlan
}

func syncDryRunGoldenCases() []syncDryRunGoldenCase {
	return []syncDryRunGoldenCase{
		{
			name: "nothing_to_do",
			plan: sync.DryRunPlan{},
		},
		{
			name: "pull_and_clean",
			plan: sync.DryRunPlan{
				PullBranch:   "main",
				PullRevision: "abc1234",
				Clean: []sync.DryRunCleanItem{
					{Branch: "feat-login", Reason: "merged into main"},
					{Branch: "feat-old", Reason: "closed on GitHub"},
				},
			},
		},
		{
			name: "restack_requested",
			plan: sync.DryRunPlan{
				RestackRequested: true,
				Restack: []sync.DryRunRestackItem{
					{Branch: "feat-api", Parent: "main"},
					{Branch: "feat-ui", Parent: "feat-api"},
				},
			},
		},
		{
			name: "full",
			plan: sync.DryRunPlan{
				RestackRequested: true,
				PullBranch:       "main",
				PullRevision:     "abc1234",
				Clean:            []sync.DryRunCleanItem{{Branch: "feat-old", Reason: "merged into main"}},
				Restack: []sync.DryRunRestackItem{
					{Branch: "feat-api", Parent: "main"},
					{Branch: "feat-ui", Parent: "feat-api"},
				},
				SkippedStacks: []string{"feat-wip"},
			},
		},
	}
}

func TestSyncDryRunGolden(t *testing.T) {
	t.Parallel()
	for _, c := range syncDryRunGoldenCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			buf := &bytes.Buffer{}
			renderSyncDryRunText(output.NewConsoleOutput(buf, false), c.plan)
			golden.Assert(t, filepath.Join("testdata", "sync", "dryrun", c.name+".golden"), golden.StripANSI(buf.String()))
		})
	}
}
