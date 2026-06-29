// Package stacklog gathers the current stack's commit band for the stack-aware
// `stackit log` view: the commits on the branch you're standing on and its
// ancestors down to (but not including) trunk, ordered top-down from HEAD. It
// also surfaces the branch/tag decorations and trunk-tip marker the renderer
// needs to draw a git-log-style view with a clear trunk boundary. The result is
// presentation-agnostic so CLI/JSON adapters can render it however they need.
package stacklog

import (
	"strings"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// Source is the narrow engine dependency this action needs. engine.Engine
// satisfies it.
type Source interface {
	CurrentBranch() *engine.Branch
	Trunk() engine.Branch
	Graph(strategy engine.SortStrategy) *engine.StackGraph
	BatchCommits(branches engine.Branches, format engine.CommitFormat) map[string][]string
	RefDecorations() (map[string][]git.RefDecoration, error)
	GetRevisionForName(branchName string) (string, error)
}

// Commit is one commit on a stack branch, identified by its full SHA so the
// renderer can join it against the decoration map.
type Commit struct {
	SHA     string
	Subject string
}

// Branch is one branch in the current stack's ancestor path, with its own
// commits ordered newest-first (the branch tip first).
type Branch struct {
	Name      string
	IsCurrent bool
	Commits   []Commit
}

// Result is the gathered stack band.
//
// Branches are ordered top-down: the branch you're standing on first, its
// trunk-most ancestor last; trunk itself is excluded (it's the boundary). When
// HEAD is on trunk or detached, Branches is empty, so callers render trunk
// history alone.
//
// Decorations is keyed by full commit SHA and covers every local branch head and
// tag, so the renderer can annotate both the stack band and the trunk band from
// one map.
type Result struct {
	Branches    []Branch
	TrunkName   string
	TrunkTipSHA string // full SHA of the trunk tip, for the boundary marker
	Decorations map[string][]git.RefDecoration
}

// Gather collects the current stack's ancestor-path commits plus the decoration
// map and trunk-tip marker. It never walks above HEAD (children are out of
// scope) and excludes trunk from the band.
func Gather(src Source) (Result, error) {
	decorations, err := src.RefDecorations()
	if err != nil {
		return Result{}, err
	}

	trunk := src.Trunk()
	trunkTip, err := src.GetRevisionForName(trunk.GetName())
	if err != nil {
		return Result{}, err
	}

	res := Result{
		TrunkName:   trunk.GetName(),
		TrunkTipSHA: trunkTip,
		Decorations: decorations,
	}

	current := src.CurrentBranch()
	if current == nil || current.IsTrunk() {
		return res, nil
	}

	// Downstack returns the ancestor path trunk-ward→current (trunk excluded).
	// Reverse it so the branch we're standing on renders at the top.
	branches := src.Graph(engine.SortStrategyAlphabetical).Downstack(*current, true).Reverse()
	if len(branches) == 0 {
		// Current branch is untracked / not in the graph: no stack band to show.
		return res, nil
	}

	// One combined walk per branch yields both SHA and subject on each record,
	// so the two never desync (an empty subject can't shift the pairing).
	commitsByBranch := src.BatchCommits(branches, engine.CommitFormatSHASubject)

	currentName := current.GetName()
	for _, b := range branches {
		name := b.GetName()
		res.Branches = append(res.Branches, Branch{
			Name:      name,
			IsCurrent: name == currentName,
			Commits:   parseCommits(commitsByBranch[name]),
		})
	}
	return res, nil
}

// parseCommits splits each CommitFormatSHASubject record ("<full-sha>\x00<subject>")
// into a Commit. A record with a trailing empty subject still yields a Commit with
// an empty Subject (it is not dropped).
func parseCommits(records []string) []Commit {
	commits := make([]Commit, 0, len(records))
	for _, rec := range records {
		sha, subject, _ := strings.Cut(rec, "\x00")
		commits = append(commits, Commit{SHA: sha, Subject: subject})
	}
	return commits
}
