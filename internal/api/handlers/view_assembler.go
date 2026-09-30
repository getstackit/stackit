package handlers

import (
	"context"

	"github.com/getstackit/stackit/internal/actions/stackview"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// ViewGitHub is the GitHub surface the /view payload reads: CI checks, PR
// titles, and the repo/user identity.
type ViewGitHub interface {
	github.ChecksReader
	github.Identity
	BatchGetPRTitles(ctx context.Context, prNumbers []git.PRNumber) (map[git.PRNumber]string, error)
}

// ViewEngine is the engine surface the /view payload reads: stack structure,
// the batched per-branch data, mapper lookups, and recent trunk commits.
type ViewEngine interface {
	engine.StackView
	stackview.BranchDataReader
	httpcontract.MapperReader
	GetRecentTrunkCommits(count int) ([]git.RecentCommit, error)
}

// ViewAssembler builds the combined /view payload.
type ViewAssembler struct {
	eng        ViewEngine
	gh         ViewGitHub
	remote     string
	visibility Visibility
}

func NewViewAssembler(eng ViewEngine, gh ViewGitHub, remote string, visibility Visibility) *ViewAssembler {
	return &ViewAssembler{
		eng:        eng,
		gh:         gh,
		remote:     remote,
		visibility: visibility,
	}
}

func (a *ViewAssembler) Build(ctx context.Context) (httpcontract.ViewResponse, error) {
	stacks := stackview.DiscoverStacksWithSort(a.eng, engine.SortStrategySmart)

	graph := a.eng.Graph(engine.SortStrategySmart)
	checksMap := a.fetchChecks(ctx, stacks)
	details := a.mapStackDetails(ctx, graph, stacks, checksMap)

	recentlyMerged := a.fetchRecentlyMerged(ctx)

	return httpcontract.ViewResponse{
		Repo:           a.buildRepo(ctx),
		Stacks:         details,
		RecentlyMerged: recentlyMerged,
	}, nil
}

func (a *ViewAssembler) buildRepo(ctx context.Context) httpcontract.RepoResponse {
	owner, repo := "", ""
	var currentUser string
	if a.gh != nil {
		ghRepo := a.gh.Repo()
		owner, repo = ghRepo.Owner, ghRepo.Name
		// currentUser identifies the operator (it comes from the server's
		// GitHub token). On a public read-only server we must not leak that,
		// and we must not spend the operator's GitHub rate limit on
		// anonymous reads — so the lookup is skipped entirely.
		if a.visibility == VisibilityPrivate {
			currentUser, _ = a.gh.GetCurrentUser(ctx)
		}
	}

	return httpcontract.RepoResponse{
		Owner:         owner,
		Repo:          repo,
		Trunk:         a.eng.Trunk().GetName(),
		CurrentBranch: a.eng.CurrentBranchName(),
		Remote:        a.remote,
		CurrentUser:   currentUser,
	}
}

func (a *ViewAssembler) fetchChecks(ctx context.Context, stacks stackview.Stacks) github.ChecksByBranch {
	if a.gh == nil {
		return nil
	}

	allBranches := stacks.AllBranchNames()
	if len(allBranches) == 0 {
		return nil
	}

	checksMap, _ := a.gh.BatchGetPRChecksStatus(ctx, allBranches)
	return checksMap
}

func (a *ViewAssembler) mapStackDetails(
	ctx context.Context,
	graph *engine.StackGraph,
	stacks stackview.Stacks,
	checksMap github.ChecksByBranch,
) []httpcontract.StackDetail {
	// One batch pass over every branch in every stack, not one per stack —
	// see stackview.BranchData.
	branches := stackview.BranchesFromGraph(graph, stacks.AllBranchNames())
	data := stackview.FetchBranchData(ctx, a.eng, branches)

	details := make([]httpcontract.StackDetail, 0, len(stacks))
	for _, stack := range stacks {
		detail := httpcontract.MapStackDetail(a.eng, graph, stackInput(stack), checksMap, data)
		details = append(details, detail)
	}
	return details
}

// stackInput converts a discovered stack into the mapper's input shape.
func stackInput(stack stackview.StackInfo) httpcontract.StackInput {
	return httpcontract.StackInput{
		RootBranch:  stack.RootBranch,
		AllBranches: stack.AllBranches,
		PRCount:     stack.PRCount,
		Scope:       engine.NewScope(stack.Scope),
	}
}

func (a *ViewAssembler) fetchRecentlyMerged(ctx context.Context) []httpcontract.TrunkCommitResponse {
	recentCommits, err := a.eng.GetRecentTrunkCommits(10)
	if err != nil || len(recentCommits) == 0 {
		return nil
	}

	prTitles := a.fetchPRTitles(ctx, recentCommits)
	return httpcontract.MapTrunkCommits(recentCommits, prTitles)
}

// fetchPRTitles collects all unique PR numbers from stack-merge commits and
// batch-fetches their titles from GitHub. Returns nil on error or if no GitHub client.
func (a *ViewAssembler) fetchPRTitles(ctx context.Context, commits []git.RecentCommit) map[git.PRNumber]string {
	if a.gh == nil {
		return nil
	}

	// PR-number collection is shared with the `stackit log` command via internal/git.
	prNumbers := git.RecentCommits(commits).PRTitleNumbers()
	if len(prNumbers) == 0 {
		return nil
	}

	titles, err := a.gh.BatchGetPRTitles(ctx, prNumbers)
	if err != nil {
		return nil
	}
	return titles
}
