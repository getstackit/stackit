// Package stacklog gathers the current stack's commit band for the stack-aware
// `stackit log` view: the commits on the branch you're standing on and its
// ancestors down to (but not including) trunk, ordered top-down from HEAD. It
// also surfaces the branch/tag decorations and trunk-tip marker the renderer
// needs to draw a git-log-style view with a clear trunk boundary. The result is
// presentation-agnostic so CLI/JSON adapters can render it however they need.
package stacklog

import (
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

// Decoration is a local branch or tag pointing at a commit.
type Decoration struct {
	Name  string
	IsTag bool
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
// HEAD is on trunk or detached, OnTrunk is true and Branches is empty, so
// callers render trunk history alone.
//
// Decorations is keyed by full commit SHA and covers every local branch head and
// tag, so the renderer can annotate both the stack band and the trunk band from
// one map.
type Result struct {
	Branches    []Branch
	TrunkName   string
	TrunkTipSHA string // full SHA of the trunk tip, for the boundary marker
	OnTrunk     bool
	Decorations map[string][]Decoration
}

// Gather collects the current stack's ancestor-path commits plus the decoration
// map and trunk-tip marker. It never walks above HEAD (children are out of
// scope) and excludes trunk from the band.
func Gather(src Source) (Result, error) {
	decorations, err := decorationMap(src)
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
		res.OnTrunk = true
		return res, nil
	}

	// Downstack returns the ancestor path trunk-ward→current (trunk excluded).
	// Reverse it so the branch we're standing on renders at the top.
	chain := src.Graph(engine.SortStrategyAlphabetical).Downstack(*current, true)
	branches := reversed(chain)
	if len(branches) == 0 {
		// Current branch is untracked / not in the graph: no stack band to show.
		return res, nil
	}

	shaByBranch := src.BatchCommits(branches, engine.CommitFormatSHA)
	subjectByBranch := src.BatchCommits(branches, engine.CommitFormatSubject)

	currentName := current.GetName()
	for _, b := range branches {
		name := b.GetName()
		res.Branches = append(res.Branches, Branch{
			Name:      name,
			IsCurrent: name == currentName,
			Commits:   zipCommits(shaByBranch[name], subjectByBranch[name]),
		})
	}
	return res, nil
}

// decorationMap reads ref decorations and converts them to the action type.
func decorationMap(src Source) (map[string][]Decoration, error) {
	raw, err := src.RefDecorations()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]Decoration, len(raw))
	for sha, refs := range raw {
		converted := make([]Decoration, len(refs))
		for i, r := range refs {
			converted[i] = Decoration{Name: r.Name, IsTag: r.IsTag}
		}
		out[sha] = converted
	}
	return out, nil
}

// zipCommits pairs full SHAs with subjects. Both lists come from walking the
// same branch range in the same (newest-first) order, so they align by index;
// any length mismatch is defensive and simply truncates to the shorter list.
func zipCommits(shas, subjects []string) []Commit {
	n := min(len(shas), len(subjects))
	commits := make([]Commit, n)
	for i := range n {
		commits[i] = Commit{SHA: shas[i], Subject: subjects[i]}
	}
	return commits
}

// reversed returns the branches in reverse order, so a trunk-ward→current chain
// becomes current→trunk-ward (top-down for display).
func reversed(branches engine.Branches) engine.Branches {
	out := make([]engine.Branch, len(branches))
	for i, b := range branches {
		out[len(branches)-1-i] = b
	}
	return engine.NewBranches(out)
}
