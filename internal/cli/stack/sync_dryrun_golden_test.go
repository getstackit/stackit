package stack

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers/golden"
)

// TestDryRunPlanToResult locks the JSON contract: the rich preview plan must
// project to a names-only DryRunResult, dropping the display detail (target
// revision, deletion reason, restack parent) that only the text preview uses.
func TestDryRunPlanToResult(t *testing.T) {
	t.Parallel()
	plan := dryRunPlan{
		pullBranch:    "main",
		pullRev:       "abc1234",
		clean:         []dryRunCleanItem{{branch: "feat-old", reason: "merged into main"}},
		restack:       []dryRunRestackItem{{branch: "feat-api", parent: "main"}},
		restackStacks: []string{"feat-api"},
		skipped:       []string{"feat-wip"},
	}
	result := plan.toResult()
	require.Equal(t, "main", result.WouldPull)
	require.Equal(t, []string{"feat-old"}, result.WouldClean)
	require.Equal(t, []string{"feat-api"}, result.WouldRestack)
	require.Equal(t, []string{"feat-api"}, result.WouldRestackStacks)
	require.Equal(t, []string{"feat-wip"}, result.SkippedStacks)
}

// Golden coverage for the human-readable `sync --dry-run` preview. The dry-run
// is a read-only query, so its renderer takes a plain dryRunPlan — no engine, no
// repository. See sync.go:computeSyncDryRun for where the plan is built.

type syncDryRunGoldenCase struct {
	name string
	plan dryRunPlan
}

func syncDryRunGoldenCases() []syncDryRunGoldenCase {
	return []syncDryRunGoldenCase{
		{
			name: "nothing_to_do",
			plan: dryRunPlan{},
		},
		{
			name: "pull_and_clean",
			plan: dryRunPlan{
				pullBranch: "main",
				pullRev:    "abc1234",
				clean: []dryRunCleanItem{
					{branch: "feat-login", reason: "merged into main"},
					{branch: "feat-old", reason: "closed on GitHub"},
				},
			},
		},
		{
			name: "restack_requested",
			plan: dryRunPlan{
				restacked: true,
				restack: []dryRunRestackItem{
					{branch: "feat-api", parent: "main"},
					{branch: "feat-ui", parent: "feat-api"},
				},
			},
		},
		{
			name: "full",
			plan: dryRunPlan{
				restacked:  true,
				pullBranch: "main",
				pullRev:    "abc1234",
				clean:      []dryRunCleanItem{{branch: "feat-old", reason: "merged into main"}},
				restack: []dryRunRestackItem{
					{branch: "feat-api", parent: "main"},
					{branch: "feat-ui", parent: "feat-api"},
				},
				skipped: []string{"feat-wip"},
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
