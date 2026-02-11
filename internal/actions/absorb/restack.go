package absorb

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/engine"
)

// RestackMode controls which branches are restacked after absorb.
type RestackMode string

const (
	RestackAll     RestackMode = "all"
	RestackCurrent RestackMode = "current"
	RestackScope   RestackMode = "scope"
	RestackNone    RestackMode = "none"
)

func (m RestackMode) Validate() error {
	switch m {
	case RestackAll, RestackCurrent, RestackScope, RestackNone:
		return nil
	default:
		return fmt.Errorf("invalid restack mode %q (valid: all, current, scope, none)", string(m))
	}
}

// NormalizeRestackMode canonicalizes user-supplied restack mode input,
// defaulting to RestackAll when empty and folding case so `--restack ALL`
// works.
func NormalizeRestackMode(mode RestackMode) RestackMode {
	normalized := RestackMode(strings.ToLower(strings.TrimSpace(string(mode))))
	if normalized == "" {
		return RestackAll
	}
	return normalized
}

func selectRestackBranches(graph *engine.StackGraph, eng engine.Engine, mode RestackMode, currentBranchName, oldestModifiedBranch string, currentScope engine.Scope) []engine.Branch {
	if oldestModifiedBranch == "" || mode == RestackNone {
		return nil
	}

	switch mode {
	case RestackAll:
		return graph.Range(eng.GetBranch(oldestModifiedBranch), engine.StackRange{RecursiveChildren: true})
	case RestackCurrent:
		current := eng.GetBranch(currentBranchName)
		branches := graph.Range(current, engine.StackRange{RecursiveChildren: true, IncludeCurrent: true})
		if oldestModifiedBranch == currentBranchName {
			filtered := make([]engine.Branch, 0, len(branches))
			for _, branch := range branches {
				if branch.GetName() == currentBranchName {
					continue
				}
				filtered = append(filtered, branch)
			}
			return filtered
		}
		return branches
	case RestackScope:
		if currentScope.IsEmpty() {
			return selectRestackBranches(graph, eng, RestackCurrent, currentBranchName, oldestModifiedBranch, currentScope)
		}

		branches := graph.Range(eng.GetBranch(oldestModifiedBranch), engine.StackRange{RecursiveChildren: true})
		filtered := make([]engine.Branch, 0, len(branches))
		for _, branch := range branches {
			if branch.GetScope().Equal(currentScope) {
				filtered = append(filtered, branch)
			}
		}
		return filtered
	default:
		return nil
	}
}
