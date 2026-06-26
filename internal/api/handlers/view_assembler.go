package handlers

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/merge"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// ViewAssembler builds the combined /view payload.
type ViewAssembler struct {
	eng        engine.BranchReader
	gh         github.Client
	remote     string
	visibility Visibility
}

func NewViewAssembler(eng engine.BranchReader, gh github.Client, remote string, visibility Visibility) *ViewAssembler {
	return &ViewAssembler{
		eng:        eng,
		gh:         gh,
		remote:     remote,
		visibility: visibility,
	}
}

func (a *ViewAssembler) Build(ctx context.Context) (httpcontract.ViewResponse, error) {
	stacks, err := merge.DiscoverStacksWithSort(a.eng, engine.SortStrategySmart)
	if err != nil {
		return httpcontract.ViewResponse{}, fmt.Errorf("failed to discover stacks: %w", err)
	}

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
		owner, repo = a.gh.GetOwnerRepo()
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

func (a *ViewAssembler) fetchChecks(ctx context.Context, stacks []merge.MultiStackInfo) map[string]*github.CheckStatus {
	if a.gh == nil {
		return nil
	}

	var allBranches []string
	for _, stack := range stacks {
		allBranches = append(allBranches, stack.AllBranches...)
	}
	if len(allBranches) == 0 {
		return nil
	}

	checksMap, _ := a.gh.BatchGetPRChecksStatus(ctx, allBranches)
	return checksMap
}

func (a *ViewAssembler) mapStackDetails(
	ctx context.Context,
	graph *engine.StackGraph,
	stacks []merge.MultiStackInfo,
	checksMap map[string]*github.CheckStatus,
) []httpcontract.StackDetail {
	details := make([]httpcontract.StackDetail, 0, len(stacks))
	for _, stack := range stacks {
		detail := httpcontract.MapStackDetail(ctx, a.eng, graph, stack.RootBranch, stack.AllBranches, stack.PRCount, stack.Scope, checksMap)
		details = append(details, detail)
	}
	return details
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
func (a *ViewAssembler) fetchPRTitles(ctx context.Context, commits []git.RecentCommit) map[int]string {
	if a.gh == nil {
		return nil
	}

	seen := make(map[int]struct{})
	var prNumbers []int
	for _, c := range commits {
		if c.StackSize == 0 {
			continue
		}
		// Include the consolidation PR itself so we can use its title as the display message
		if c.PRNumber != 0 {
			if _, ok := seen[c.PRNumber]; !ok {
				seen[c.PRNumber] = struct{}{}
				prNumbers = append(prNumbers, c.PRNumber)
			}
		}
		for _, pr := range c.StackPRNumbers {
			if _, ok := seen[pr]; !ok {
				seen[pr] = struct{}{}
				prNumbers = append(prNumbers, pr)
			}
		}
	}
	if len(prNumbers) == 0 {
		return nil
	}

	owner, repo := a.gh.GetOwnerRepo()
	titles, err := a.gh.BatchGetPRTitles(ctx, owner, repo, prNumbers)
	if err != nil {
		return nil
	}
	return titles
}
