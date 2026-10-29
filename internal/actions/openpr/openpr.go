// Package openpr implements the pr command, which opens a branch's pull
// request in the default browser.
package openpr

import (
	"context"
	"fmt"
	"strconv"

	"github.com/getstackit/stackit/internal/browser"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// Options configures which pull request to open.
type Options struct {
	// Target is a branch name or PR number from the command line. Empty means
	// the current branch.
	Target string
	// Stack opens the root PR of the target's stack instead of the target
	// branch's own PR.
	Stack bool
}

// Engine lists exactly the engine methods the pr command calls.
type Engine interface {
	CurrentBranch(ctx context.Context) *engine.Branch
	BranchNames() *engine.BranchSet
	AllBranches() engine.Branches
	GetBranch(branchName string) engine.Branch
	GetStackRootForBranch(branch engine.Branch) string
	BatchReadMetadataRaw(ctx context.Context, branchNames []string) (engine.MetaMap, map[string]error)
}

// Deps are the collaborators the pr command needs. Adapters build them from
// their own runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
}

// openBrowser is the function Action uses to open a URL in the user's
// default browser. Overridable via SetBrowserOpener so tests don't launch
// real browser windows.
var openBrowser = browser.Open

// SetBrowserOpener overrides the function Action uses to open a URL in
// the browser. Intended for tests; pass nil to restore the default.
func SetBrowserOpener(fn func(url string) error) {
	if fn == nil {
		fn = browser.Open
	}
	openBrowser = fn
}

// Action resolves the pull request opts identifies and opens it in the
// default browser.
func Action(ctx context.Context, deps Deps, opts Options) error {
	eng := deps.Engine

	branchName, err := resolvePRBranch(ctx, eng, opts.Target)
	if err != nil {
		return err
	}

	if opts.Stack {
		if root := eng.GetStackRootForBranch(eng.GetBranch(branchName)); root != "" {
			branchName = root
		}
	}

	prInfo, err := eng.GetBranch(branchName).PrInfo()
	if err != nil {
		return fmt.Errorf("reading pull request info for %q: %w", branchName, err)
	}
	if prInfo == nil || prInfo.URL() == "" {
		return fmt.Errorf("%q has no associated pull request; run 'stackit submit' first", branchName)
	}

	deps.Out.Info("Opening %s", prInfo.URL())
	// Best-effort: environments without a browser (SSH, headless CI) still get
	// the URL printed above, mirroring submit's --web behavior.
	if err := openBrowser(prInfo.URL()); err != nil {
		deps.Out.Debug("Failed to open browser: %v", err)
	}
	return nil
}

// resolvePRBranch turns target into a branch name. An empty target resolves to
// the current branch; a numeric target resolves to whichever tracked branch
// has that PR number; anything else is used as a branch name directly.
func resolvePRBranch(ctx context.Context, eng Engine, target string) (string, error) {
	if target == "" {
		current := eng.CurrentBranch(ctx)
		if current == nil {
			return "", errors.ErrNotOnBranch
		}
		return current.Name(), nil
	}

	number, convErr := strconv.Atoi(target)
	if convErr != nil {
		if !eng.BranchNames().Contains(target) {
			return "", fmt.Errorf("branch %q not found: %w", target, errors.ErrBranchNotFound)
		}
		return target, nil
	}

	branchName, found := findBranchByPRNumber(ctx, eng, git.PRNumber(number))
	if !found {
		return "", fmt.Errorf("no tracked branch found for pull request #%d", number)
	}
	return branchName, nil
}

// findBranchByPRNumber searches tracked branches for the one whose recorded PR
// number matches number, reading all metadata in a single batched pass rather
// than once per branch.
func findBranchByPRNumber(ctx context.Context, eng Engine, number git.PRNumber) (string, bool) {
	branchNames := eng.AllBranches().Names()
	metas, _ := eng.BatchReadMetadataRaw(ctx, branchNames)
	for _, name := range branchNames {
		meta := metas[name]
		if meta == nil {
			continue
		}
		prInfo := meta.GetPrInfo()
		if prInfo != nil && prInfo.Number != nil && *prInfo.Number == number {
			return name, true
		}
	}
	return "", false
}
