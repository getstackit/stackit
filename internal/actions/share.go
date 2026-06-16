package actions

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// DefaultShareMarker is appended to the current branch's line in shared output.
const DefaultShareMarker = "👈"

// ShareOptions configures how the stack is rendered for sharing.
type ShareOptions struct {
	// BranchName selects the stack to share. Defaults to the current branch.
	BranchName string
	// Marker indicates the current branch in the output. Defaults to
	// DefaultShareMarker.
	Marker string
}

// ShareAction renders the current stack as Slack-flavored markdown (mrkdwn)
// suitable for copy-pasting into a Slack message. Each branch is emitted as a
// bullet: branches with an open PR become a clickable link (#<number> <title>),
// while branches without a PR fall back to their branch name so the stack is
// still complete before submit. Branches are listed base-first, mirroring the
// stack navigation footer that Stackit writes into PR bodies.
func ShareAction(ctx *app.Context, opts ShareOptions) error {
	eng := ctx.Engine

	currentBranch := eng.CurrentBranch()
	branchName := opts.BranchName
	if branchName == "" {
		if currentBranch == nil {
			return errors.ErrNotOnBranch
		}
		branchName = currentBranch.GetName()
	}

	branchObj := eng.GetBranch(branchName)
	if branchObj.IsTrunk() {
		return fmt.Errorf("%q is the trunk branch; check out a branch in a stack to share it", branchName)
	}
	if !branchObj.IsTracked() {
		return fmt.Errorf("%q is not a tracked branch; check out or pass a branch that's part of a stack", branchName)
	}

	currentName := ""
	if currentBranch != nil {
		currentName = currentBranch.GetName()
	}

	marker := opts.Marker
	if marker == "" {
		marker = DefaultShareMarker
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	base := eng.GetBranch(shareStackBase(eng, branchName))

	var sb strings.Builder
	for b, depth := range eng.BranchesDepthFirst(base) {
		if b.IsTrunk() || b.IsWorktreeAnchor() {
			continue
		}
		if !graph.IsRelated(b, branchObj) {
			continue
		}
		sb.WriteString(shareLeaf(b, depth, currentName, marker))
		sb.WriteString("\n")
	}

	output := strings.TrimRight(sb.String(), "\n")
	if output == "" {
		return fmt.Errorf("no branches to share in the current stack")
	}

	ctx.Output.Info("%s", output)
	return nil
}

// shareStackBase walks up to the branch directly above trunk, which is the root
// of the visible stack.
func shareStackBase(eng engine.Engine, branchName string) string {
	branch := eng.GetBranch(branchName)
	parent := branch.GetParent()
	if parent == nil || parent.IsTrunk() {
		return branchName
	}
	return shareStackBase(eng, parent.GetName())
}

// shareLeaf renders a single branch as a Slack mrkdwn bullet, indented by depth.
func shareLeaf(branch engine.Branch, depth int, currentName, marker string) string {
	var (
		prNumber       *int
		prTitle, prURL string
	)
	if prInfo, err := branch.GetPrInfo(); err == nil && prInfo != nil {
		prNumber = prInfo.Number()
		prTitle = prInfo.Title()
		prURL = prInfo.URL()
	}

	indent := strings.Repeat("    ", depth)
	line := indent + "• " + shareLabel(branch.GetName(), prNumber, prTitle, prURL)
	if branch.GetName() == currentName {
		line += " " + marker
	}
	return line
}

// shareLabel renders the Slack mrkdwn label for a branch. A branch with an open
// PR becomes a clickable link (<url|#123 title>), falling back to bare text when
// the URL is unknown; a branch without a PR falls back to its name so the stack
// is still complete before submit.
func shareLabel(name string, prNumber *int, prTitle, prURL string) string {
	if prNumber == nil {
		return fmt.Sprintf("`%s` _(no PR)_", name)
	}

	text := fmt.Sprintf("#%d", *prNumber)
	if prTitle != "" {
		text = fmt.Sprintf("#%d %s", *prNumber, sanitizeSlackText(prTitle))
	}
	if prURL == "" {
		return text
	}
	return fmt.Sprintf("<%s|%s>", prURL, text)
}

// sanitizeSlackText escapes characters that have special meaning in Slack's
// mrkdwn format so that PR titles containing angle brackets or ampersands don't
// produce malformed links (e.g. "<url|#1 Add <T> support>" would break the
// link because the inner "<T>" looks like a nested mrkdwn element).
func sanitizeSlackText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
