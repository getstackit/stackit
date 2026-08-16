package navigation

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// Direction represents the traversal direction
type Direction string

const (
	// DirectionBottom specifies navigating towards the trunk
	DirectionBottom Direction = "BOTTOM"
	// DirectionTop specifies navigating towards the stack tips
	DirectionTop Direction = "TOP"
)

// SwitchBranchAction switches to a branch based on the given direction
func SwitchBranchAction(direction Direction, ctx *app.Context, handler Handler) (actions.CheckoutResult, error) {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	currentBranch := ctx.Engine.CurrentBranch()
	if currentBranch == nil {
		return actions.CheckoutResult{}, errors.ErrNotOnBranch
	}

	var targetBranch string
	var err error

	switch direction {
	case DirectionBottom:
		// Walks the parent chain only — no full graph needed, so the command can
		// run under LoadModeBranchesOnly and lazily promote just the chain.
		targetBranch = traverseDownward(currentBranch.GetName(), ctx)
	case DirectionTop:
		// Walking toward the tips needs child relationships, which requires the
		// full stack graph.
		graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
		targetBranch, err = traverseUpward(currentBranch.GetName(), ctx, graph, handler)
		if err != nil {
			return actions.CheckoutResult{}, err
		}
	default:
		return actions.CheckoutResult{}, fmt.Errorf("invalid direction: %s", direction)
	}

	if targetBranch == currentBranch.GetName() {
		directionText := "bottom most"
		if direction == DirectionTop {
			directionText = "top most"
		}
		ctx.Output.Info("Already at the %s branch in the stack.", directionText)
		return actions.CheckoutResult{}, nil
	}

	// Checkout the target branch
	result, err := actions.CheckoutAction(ctx, actions.CheckoutOptions{BranchName: targetBranch}, nil)
	if err != nil {
		return actions.CheckoutResult{}, fmt.Errorf("failed to checkout branch %s: %w", targetBranch, err)
	}

	return result, nil
}

// traverseDownward walks down the parent chain to find the first branch from trunk.
// It skips worktree anchor branches transparently.
func traverseDownward(currentBranch string, ctx *app.Context) string {
	currentBranchObj := ctx.Engine.GetBranch(currentBranch)
	if currentBranchObj.IsTrunk() {
		return currentBranch
	}

	parent := currentBranchObj.GetParent()
	if parent == nil {
		// No parent, we're at the bottom
		return currentBranch
	}

	// Skip worktree anchors — walk through them transparently
	for parent != nil && parent.IsWorktreeAnchor() {
		parent = parent.GetParent()
	}

	if parent == nil || parent.IsTrunk() {
		return currentBranch
	}

	ctx.Output.Info("⮑  %s", parent.GetName())
	return traverseDownward(parent.GetName(), ctx)
}

// traverseUpward walks up the children chain to find the tip branch.
// It skips worktree anchor branches transparently.
func traverseUpward(currentBranch string, ctx *app.Context, graph *engine.StackGraph, handler Handler) (string, error) {
	children := graph.ChildBranches(ctx.Engine.GetBranch(currentBranch))

	// Filter out worktree anchors, but include their children
	children = graph.FlattenThroughAnchors(children)

	if len(children) == 0 {
		// No children, we're at the tip
		return currentBranch, nil
	}

	var nextBranch string
	var err error

	if len(children) == 1 {
		// Single child, follow it
		nextBranch = children[0].GetName()
	} else {
		// Multiple children, use handler to select
		nextBranch, err = handleMultipleChildren(children.Names(), handler)
		if err != nil {
			return "", err
		}
	}

	ctx.Output.Info("⮑  %s", nextBranch)
	return traverseUpward(nextBranch, ctx, graph, handler)
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
