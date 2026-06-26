package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/create"
	"github.com/getstackit/stackit/internal/actions/doctor"
	syncaction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/actions/track"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// countingFetchRunner counts the two network-bound git operations a startup
// metadata bootstrap would use: `git fetch` (FetchRefSpecs) and `git ls-remote`
// (FetchRemoteShas). Everything else delegates to a real runner unchanged.
type countingFetchRunner struct {
	git.Runner
	fetchRefSpecs   atomic.Int64
	fetchRemoteShas atomic.Int64
}

func (c *countingFetchRunner) FetchRefSpecs(ctx context.Context, remote string, refspecs []string) error {
	c.fetchRefSpecs.Add(1)
	return c.Runner.FetchRefSpecs(ctx, remote, refspecs)
}

func (c *countingFetchRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	c.fetchRemoteShas.Add(1)
	return c.Runner.FetchRemoteShas(ctx, remote)
}

func (c *countingFetchRunner) totalFetches() int64 {
	return c.fetchRefSpecs.Load() + c.fetchRemoteShas.Load()
}

// remoteMetadataScenario builds a repo whose remote carries stackit metadata
// refs but whose local clone has NOT configured the metadata fetch refspec —
// the exact trap that made pre-fix engine construction auto-fetch on every
// command (#1330). Commands run against this state must stay offline at
// startup; only sync should reach the remote, and only in its sync phase.
func remoteMetadataScenario(t *testing.T) *scenario.Scenario {
	t.Helper()
	s := scenario.NewRemoteScenario(t)
	s.CreateBranch("feature").CommitChange("f.txt", "f").TrackBranch("feature", "main")
	require.NoError(t, s.Scene.Repo.RunGitCommand("push", "origin", "refs/stackit/metadata/feature"))
	require.NoError(t, s.Scene.Repo.RunGitCommand("update-ref", "-d", "refs/stackit/remote-metadata/feature"))
	// Ensure the metadata refspec is absent so a re-introduced auto-fetch would
	// have a reason to fire (and be caught here). Ignore the error when it is
	// already unset.
	_ = s.Scene.Repo.RunGitCommand("config", "--unset-all", "remote.origin.fetch", "stackit/metadata")
	s.Checkout("main")
	return s
}

// countingContext builds a fresh engine over the scenario repo backed by a
// fetch-counting runner, so a test can attribute every remote read to the
// command under test.
func countingContext(t *testing.T, s *scenario.Scenario) (*app.Context, *countingFetchRunner) {
	t.Helper()
	counting := &countingFetchRunner{Runner: git.NewRunnerWithPath(s.Scene.Dir, nil)}
	eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Scene.Dir, Trunk: "main", Git: counting})
	require.NoError(t, err)
	ctx := app.NewContext(eng, app.WithRepoRoot(s.Scene.Dir), app.WithWriter(&bytes.Buffer{}))
	return ctx, counting
}

func TestCreateDoesNotFetchAtStartup(t *testing.T) {
	t.Parallel()
	s := remoteMetadataScenario(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.Scene.Dir, "new.txt"), []byte("x"), 0o644))
	require.NoError(t, s.Scene.Repo.RunGitCommand("add", "-A"))

	ctx, counting := countingContext(t, s)
	_, err := create.Action(ctx, create.Options{Message: "feat: new"}, nil)
	require.NoError(t, err)

	require.Zero(t, counting.totalFetches(), "create must not fetch remote metadata at startup")
}

func TestTrackDoesNotFetchAtStartup(t *testing.T) {
	t.Parallel()
	s := remoteMetadataScenario(t)
	require.NoError(t, s.Scene.Repo.RunGitCommand("branch", "extra", "main"))

	ctx, counting := countingContext(t, s)
	require.NoError(t, track.Action(ctx, track.Options{BranchName: "extra", Parent: "main"}, nil))

	require.Zero(t, counting.totalFetches(), "track must not fetch remote metadata at startup")
}

func TestTreeDoesNotFetchAtStartup(t *testing.T) {
	t.Parallel()
	s := remoteMetadataScenario(t)

	ctx, counting := countingContext(t, s)
	require.NoError(t, actions.TreeAction(ctx, actions.TreeOptions{Style: actions.TreeStyleNormal}))

	require.Zero(t, counting.totalFetches(), "tree (read-only nav) must not fetch remote metadata")
}

func TestDoctorDoesNotFetchAtStartup(t *testing.T) {
	t.Parallel()
	s := remoteMetadataScenario(t)

	ctx, counting := countingContext(t, s)
	// doctor's verdict (return value) is irrelevant here; we only assert it does
	// no remote metadata fetch at startup.
	_ = doctor.Action(ctx, doctor.Options{Trunk: "main"}, nil)

	require.Zero(t, counting.totalFetches(), "doctor must not fetch remote metadata at startup")
}

func TestSyncFetchesDuringSyncPhase(t *testing.T) {
	t.Parallel()
	s := remoteMetadataScenario(t)

	ctx, counting := countingContext(t, s)
	require.NoError(t, syncaction.Action(ctx, syncaction.Options{}, nil))

	require.GreaterOrEqual(t, counting.fetchRefSpecs.Load(), int64(1),
		"sync must fetch from the remote during its sync phase")
}
