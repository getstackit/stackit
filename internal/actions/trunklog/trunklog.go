// Package trunklog gathers stack-aware trunk commit history for the `stackit log`
// command. It reads trunk commits (a range or the most recent N), collapses
// consolidated stacks into a single entry, and enriches PR titles from the forge
// when available. The result is presentation-agnostic so CLI/JSON adapters can
// render it however they need.
package trunklog

import (
	"context"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// CommitSource is the narrow engine dependency this action needs.
type CommitSource interface {
	GetRecentTrunkCommits(count int) ([]git.RecentCommit, error)
	GetTrunkCommitsInRange(from, to string) ([]git.RecentCommit, error)
}

// TitleResolver resolves PR titles from the forge. It is nil-safe: pass a nil
// resolver when no GitHub client is available (offline) and PR titles are simply
// omitted rather than treated as an error.
type TitleResolver interface {
	GetOwnerRepo() (owner, repo string)
	BatchGetPRTitles(ctx context.Context, owner, repo string, prNumbers []int) (map[int]string, error)
}

// Request selects which trunk commits to gather.
type Request struct {
	From  string // lower-bound ref; empty selects the default recent-count view
	To    string // upper-bound ref; empty defaults to the trunk tip (used with From)
	Count int    // max commits for the default view; ignored when From is set
}

// Commit is one collapsed trunk entry, decoupled from the git and HTTP types.
type Commit struct {
	SHA           string
	Message       string // PR title for enriched stack-merges, else the commit subject
	Author        string
	Date          time.Time
	Kind          git.RecentCommitKind
	PRNumber      int
	StackSize     int
	StackPRs      []int
	StackPRTitles map[int]string
	StackScope    string
}

// Result is the gathered, collapsed, enriched trunk history.
type Result struct {
	Commits []Commit
	From    string
	To      string
}

// Gather fetches trunk commits (a range when req.From is set, otherwise the most
// recent req.Count), collapses consolidated stacks, and enriches PR titles when a
// resolver is available. A nil resolver, missing owner/repo, or a forge error all
// degrade gracefully to commits without PR titles rather than failing.
func Gather(ctx context.Context, src CommitSource, titles TitleResolver, req Request) (Result, error) {
	var (
		raw []git.RecentCommit
		err error
	)
	if req.From != "" {
		raw, err = src.GetTrunkCommitsInRange(req.From, req.To)
	} else {
		raw, err = src.GetRecentTrunkCommits(req.Count)
	}
	if err != nil {
		return Result{}, err
	}

	collapsed := git.CollapseStackMerges(raw)
	prTitles := resolveTitles(ctx, titles, collapsed)

	result := Result{
		From:    req.From,
		To:      req.To,
		Commits: make([]Commit, 0, len(collapsed)),
	}
	for _, c := range collapsed {
		result.Commits = append(result.Commits, toCommit(c, prTitles))
	}
	return result, nil
}

// resolveTitles returns PR-number → title for every PR referenced by the commits,
// or nil when titles cannot be resolved (no resolver, no PRs, or a forge error).
func resolveTitles(ctx context.Context, titles TitleResolver, commits []git.RecentCommit) map[int]string {
	if titles == nil {
		return nil
	}
	prNumbers := collectStackPRNumbers(commits)
	if len(prNumbers) == 0 {
		return nil
	}
	owner, repo := titles.GetOwnerRepo()
	if owner == "" || repo == "" {
		return nil
	}
	resolved, err := titles.BatchGetPRTitles(ctx, owner, repo, prNumbers)
	if err != nil {
		return nil
	}
	return resolved
}

// collectStackPRNumbers gathers the unique PR numbers referenced by the commits:
// each commit's own PR plus the constituent PRs of stack-merges.
func collectStackPRNumbers(commits []git.RecentCommit) []int {
	seen := make(map[int]struct{})
	var nums []int
	add := func(n int) {
		if n == 0 {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		nums = append(nums, n)
	}
	for _, c := range commits {
		add(c.PRNumber)
		for _, pr := range c.StackPRNumbers {
			add(pr)
		}
	}
	return nums
}

// toCommit maps a git.RecentCommit to the action Commit, substituting the
// consolidation PR's title for the raw merge subject and attaching constituent
// PR titles when available.
func toCommit(c git.RecentCommit, prTitles map[int]string) Commit {
	message := c.Subject
	if c.StackSize > 0 && c.PRNumber != 0 && len(prTitles) > 0 {
		if title, ok := prTitles[c.PRNumber]; ok {
			message = title
		}
	}

	out := Commit{
		SHA:        c.SHA,
		Message:    message,
		Author:     c.Author,
		Date:       c.Date,
		Kind:       c.Kind,
		PRNumber:   c.PRNumber,
		StackSize:  c.StackSize,
		StackPRs:   append([]int(nil), c.StackPRNumbers...),
		StackScope: c.StackScope,
	}

	if c.StackSize > 0 && len(prTitles) > 0 {
		stackTitles := make(map[int]string)
		for _, pr := range c.StackPRNumbers {
			if title, ok := prTitles[pr]; ok {
				stackTitles[pr] = title
			}
		}
		if len(stackTitles) > 0 {
			out.StackPRTitles = stackTitles
		}
	}

	return out
}
