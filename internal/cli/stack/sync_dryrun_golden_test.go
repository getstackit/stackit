package stack

import (
	"bytes"
	"path/filepath"
	"testing"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/testhelpers/golden"
)

// Golden coverage for the human-readable `sync --dry-run` preview. The dry-run
// is a read-only query, so its renderer takes a plain DryRunResult — no engine,
// no repository. See sync.go:computeSyncDryRun for where the result is built.

type syncDryRunGoldenCase struct {
	name             string
	result           syncAction.DryRunResult
	restackRequested bool
}

func syncDryRunGoldenCases() []syncDryRunGoldenCase {
	return []syncDryRunGoldenCase{
		{
			name:   "nothing_to_do",
			result: syncAction.DryRunResult{},
		},
		{
			name: "pull_and_clean",
			result: syncAction.DryRunResult{
				WouldPull:  "main",
				WouldClean: []string{"feat-login", "feat-old"},
			},
		},
		{
			name:             "restack_requested",
			restackRequested: true,
			result: syncAction.DryRunResult{
				WouldRestack: []string{"feat-api", "feat-ui"},
			},
		},
		{
			name:             "full",
			restackRequested: true,
			result: syncAction.DryRunResult{
				WouldPull:     "main",
				WouldClean:    []string{"feat-old"},
				WouldRestack:  []string{"feat-api", "feat-ui"},
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
			renderSyncDryRunText(output.NewConsoleOutput(buf, false), c.result, c.restackRequested)
			golden.Assert(t, filepath.Join("testdata", "sync", "dryrun", c.name+".golden"), golden.StripANSI(buf.String()))
		})
	}
}
