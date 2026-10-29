package navigation

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// Direction represents the traversal direction
type Direction string

const (
	// DirectionBottom specifies navigating towards the trunk
	DirectionBottom Direction = "BOTTOM"
	// DirectionTop specifies navigating towards the stack tips
	DirectionTop Direction = "TOP"
)

// Engine lists exactly the engine methods navigation calls, including those
// of the checkout it finishes with.
type Engine interface {
	checkout.Engine
}

// Deps are the collaborators navigation needs. Adapters build them from their
// own runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
	// Log, Quiet and Here pass through to the checkout of the target branch.
	Log   output.Logger
	Quiet bool
	Here  actions.Here
}

// SwitchBranchAction switches to a branch based on the given direction
func SwitchBranchAction(ctx context.Context, deps Deps, direction Direction, handler Handler) (checkout.Result, error) {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := deps.Engine
	currentBranch := eng.CurrentBranch(ctx)
	if currentBranch == nil {
		return checkout.Result{}, errors.ErrNotOnBranch
	}

	var targetBranch string
	var err error

	switch direction {
	case DirectionBottom:
		// Walks the parent chain only — no full graph needed, so the command can
		// run under LoadModeBranchesOnly and lazily promote just the chain.
		targetBranch = traverseDownward(currentBranch.Name(), eng, deps.Out)
	case DirectionTop:
		// Walking toward the tips needs child relationships, which requires the
		// full stack graph.
		graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
		targetBranch, err = traverseUpward(currentBranch.Name(), eng, deps.Out, graph, handler)
		if err != nil {
			return checkout.Result{}, err
		}
	default:
		return checkout.Result{}, fmt.Errorf("invalid direction: %s", direction)
	}

	if targetBranch == currentBranch.Name() {
		directionText := "bottom most"
		if direction == DirectionTop {
			directionText = "top most"
		}
		deps.Out.Info("Already at the %s branch in the stack.", directionText)
		return checkout.Result{}, nil
	}

	// Checkout the target branch
	result, err := checkout.Action(ctx, checkout.Deps{
		Engine: deps.Engine,
		Out:    deps.Out,
		Log:    deps.Log,
		Quiet:  deps.Quiet,
		Here:   deps.Here,
	}, checkout.Options{BranchName: targetBranch}, nil)
	if err != nil {
		return checkout.Result{}, fmt.Errorf("failed to checkout branch %s: %w", targetBranch, err)
	}

	return result, nil
}

// traverseDownward walks down the parent chain to find the first branch from trunk.
// It skips worktree anchor branches transparently.
func traverseDownward(currentBranch string, eng engine.BranchLookup, out output.Output) string {
	currentBranchObj := eng.GetBranch(currentBranch)
	if currentBranchObj.IsTrunk() {
		return currentBranch
	}

	parent := currentBranchObj.Parent()
	if parent == nil {
		// No parent, we're at the bottom
		return currentBranch
	}

	// Skip worktree anchors — walk through them transparently
	for parent != nil && parent.IsWorktreeAnchor() {
		parent = parent.Parent()
	}

	if parent == nil || parent.IsTrunk() {
		return currentBranch
	}

	out.Info("⮑  %s", parent.Name())
	return traverseDownward(parent.Name(), eng, out)
}

// traverseUpward walks up the children chain to find the tip branch.
// It skips worktree anchor branches transparently.
func traverseUpward(currentBranch string, eng engine.BranchLookup, out output.Output, graph *engine.StackGraph, handler Handler) (string, error) {
	children := graph.ChildBranches(eng.GetBranch(currentBranch))

	// Filter out worktree anchors, but include their children
	children = FlattenThroughAnchors(children, graph)

	if len(children) == 0 {
		// No children, we're at the tip
		return currentBranch, nil
	}

	var nextBranch string
	var err error

	if len(children) == 1 {
		// Single child, follow it
		nextBranch = children[0].Name()
	} else {
		// Multiple children, use handler to select
		nextBranch, err = handleMultipleChildren(children.Names(), handler)
		if err != nil {
			return "", err
		}
	}

	out.Info("⮑  %s", nextBranch)
	return traverseUpward(nextBranch, eng, out, graph, handler)
}

// FlattenThroughAnchors replaces worktree anchor branches with their non-anchor children.
func FlattenThroughAnchors(branches engine.Branches, graph *engine.StackGraph) engine.Branches {
	result := engine.Branches{}
	for _, b := range branches {
		if b.IsWorktreeAnchor() {
			// Replace anchor with its children (recursively flatten)
			grandchildren := graph.ChildBranches(b)
			result = result.Concat(FlattenThroughAnchors(grandchildren, graph))
		} else {
			result = result.Append(b)
		}
	}
	return result
}

// handleMultipleChildren prompts the user to select a branch when multiple children exist
func handleMultipleChildren(children []string, handler Handler) (string, error) {
	if !handler.IsInteractive() {
		return "", fmt.Errorf("multiple branches found; cannot get top branch in non-interactive mode. Multiple choices available:\n%s", formatBranchList(children))
	}

	return handler.PromptSelectBranch("Multiple branches found at the same level. Select a branch to guide the navigation:", children)
}

// formatBranchList formats a list of branches for error messages
func formatBranchList(branches []string) string {
	var builder strings.Builder
	for _, branch := range branches {
		builder.WriteString(branch)
		builder.WriteString("\n")
	}
	return builder.String()
}
