package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
)

// drainFakeMergeAPI stands in for the GitHub merge API. Three of the real
// implementation's methods reach GitHub's GraphQL API by shelling out to `gh`
// through the git runner, so a mock github.Client alone is not enough to drive
// the drain loop — see newPRMergeAPI.
//
// mergePR simulates GitHub landing the PR: it fast-forwards the remote's main
// to the merged branch, which is what makes the next loop iteration observe the
// merge after drain syncs trunk.
type drainFakeMergeAPI struct {
	repo   *testhelpers.GitRepo
	merged []string
}

func (f *drainFakeMergeAPI) getMergeableState(_ context.Context, _ git.Runner, _ string) (*github.PRMergeableState, error) {
	return &github.PRMergeableState{State: git.PRStateOpen, Mergeable: true, MergeStateText: "CLEAN"}, nil
}

func (f *drainFakeMergeAPI) enableAutoMerge(_ context.Context, _ git.Runner, _ string, _ github.MergeMethod) error {
	return nil
}

func (f *drainFakeMergeAPI) waitForPRMerge(_ context.Context, _ git.Runner, _ string, _, _ time.Duration) error {
	return nil
}

func (f *drainFakeMergeAPI) waitForMergeable(_ context.Context, _ git.Runner, _ string, _, _ time.Duration) (*github.PRMergeableState, error) {
	return &github.PRMergeableState{State: git.PRStateOpen, Mergeable: true, MergeStateText: "CLEAN"}, nil
}

func (f *drainFakeMergeAPI) mergePR(_ context.Context, branchName string, _ github.MergeMethod) error {
	f.merged = append(f.merged, branchName)
	// Land the branch on the remote's trunk, the way a real merge would.
	return f.repo.RunGitCommand("push", "origin", branchName+":main")
}

// seedMockPR registers an open PR with the mock server under every lookup the
// drain loop uses: by branch (planning) and by number (which needs a NodeID,
// since drain refuses a PR without one).
func seedMockPR(cfg *testhelpers.MockGitHubServerConfig, branch, base string, number int) {
	pr := &gogithub.PullRequest{
		Number:  gogithub.Ptr(number),
		NodeID:  gogithub.Ptr(fmt.Sprintf("PR_node%d", number)),
		State:   gogithub.Ptr("open"),
		Title:   gogithub.Ptr("PR for " + branch),
		HTMLURL: gogithub.Ptr(fmt.Sprintf("https://github.com/owner/repo/pull/%d", number)),
		Head:    &gogithub.PullRequestBranch{Ref: gogithub.Ptr(branch)},
		Base:    &gogithub.PullRequestBranch{Ref: gogithub.Ptr(base)},
	}
	cfg.PRs[branch] = pr
	cfg.UpdatedPRs[number] = pr
}

// remoteMetadataParent reads the parent a branch's metadata records ON THE
// REMOTE. Reading the local ref would hide the bug entirely: drain reparents
// locally either way, and the whole failure is that the local change never
// reaches the remote.
func remoteMetadataParent(t *testing.T, repo *testhelpers.GitRepo, remoteDir, branch string) string {
	t.Helper()

	out, err := repo.RunGitCommandAndGetOutput("ls-remote", remoteDir, "refs/stackit/metadata/"+branch)
	require.NoError(t, err)
	if strings.TrimSpace(out) == "" {
		return "" // no metadata ref pushed at all
	}
	sha := strings.Fields(strings.TrimSpace(out))[0]

	// The blob is only fetchable once we have the object locally.
	require.NoError(t, repo.RunGitCommand("fetch", remoteDir, "refs/stackit/metadata/"+branch))
	blob, err := repo.RunGitCommandAndGetOutput("cat-file", "-p", sha)
	require.NoError(t, err)

	var meta struct {
		ParentBranchName string `json:"parentBranchName"`
	}
	require.NoError(t, json.Unmarshal([]byte(blob), &meta))
	return meta.ParentBranchName
}

// TestDrainPushesRestackedBranchesToRemote covers the drain stall: after each
// merge, drain restacks the remaining branches onto the new trunk and reparents
// the next one off the branch that just merged, but published neither.
//
// The stale metadata ref is what breaks the drain in practice. A stack-order CI
// check reads it, still sees the merged (deleted) parent, and reports the next
// PR as "not at the bottom of the stack" — leaving it unstable so automerge
// refuses it, one stall per remaining PR.
func TestDrainPushesRestackedBranchesToRemote(t *testing.T) {
	scene := testhelpers.NewRemoteSceneParallel(t)
	repo := scene.Repo

	remoteDir, err := repo.RunGitCommandAndGetOutput("remote", "get-url", "origin")
	require.NoError(t, err)
	remoteDir = strings.TrimSpace(remoteDir)

	eng, err := engine.NewEngine(engine.Options{RepoRoot: scene.Dir, Trunk: "main", Git: git.NewRunnerWithPath(scene.Dir, nil)})
	require.NoError(t, err)

	// main -> a -> b, both with open PRs and both pushed.
	for _, b := range []string{"a", "b"} {
		require.NoError(t, repo.CreateAndCheckoutBranch(b))
		require.NoError(t, repo.CreateChangeAndCommit("content "+b, b))
	}
	require.NoError(t, eng.Rebuild("main"))
	require.NoError(t, eng.TrackBranch(context.Background(), "a", "main"))
	require.NoError(t, eng.TrackBranch(context.Background(), "b", "a"))
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("a"), testhelpers.NewTestPrInfo(101)))
	require.NoError(t, eng.UpsertPrInfo(context.Background(), eng.GetBranch("b"), testhelpers.NewTestPrInfo(102)))
	require.NoError(t, repo.RunGitCommand("push", "origin", "a", "b"))
	require.NoError(t, eng.PushMetadataForBranches(context.Background(), []string{"a", "b"}))

	// Before draining, b's recorded parent on the remote is a.
	require.Equal(t, "a", remoteMetadataParent(t, repo, remoteDir, "b"),
		"precondition: b should start out parented on a")

	fake := &drainFakeMergeAPI{repo: repo}
	restore := newPRMergeAPI
	newPRMergeAPI = func(*app.Context) prMergeAPI { return fake }
	t.Cleanup(func() { newPRMergeAPI = restore })

	ctx := app.NewContext(eng,
		app.WithRepoRoot(scene.Dir),
		app.WithWriter(&bytes.Buffer{}),
	)
	mockCfg := testhelpers.NewMockGitHubServerConfig()
	seedMockPR(mockCfg, "a", "main", 101)
	seedMockPR(mockCfg, "b", "a", 102)
	mockClient, owner, mockRepo := testhelpers.NewMockGitHubClient(t, mockCfg)
	ctx.GitHubClient = testhelpers.NewMockGitHubClientInterface(mockClient, owner, mockRepo, mockCfg)

	require.NoError(t, repo.CheckoutBranch("b"))
	err = runMergeDrain(ctx, mergeDrainOptions{yes: true, count: 1})
	require.NoError(t, err)

	require.Equal(t, []string{"a"}, fake.merged, "drain should have merged only the bottom PR")

	// The point of the test: after a's merge, b was reparented onto main — and
	// that has to be visible on the remote, not just locally.
	require.Equal(t, "main", remoteMetadataParent(t, repo, remoteDir, "b"),
		"b's metadata ref on the remote must record its new parent after drain restacks it")
}
