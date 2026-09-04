package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/style"
)

// diffStatsJSON is the CLI-owned JSON shape for branch diff stats. The actions
// layer returns actions.BranchDiffStats (pure data); the json tags live here.
type diffStatsJSON struct {
	FilesChanged int `json:"files_changed"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

func toDiffStatsJSON(s actions.BranchDiffStats) diffStatsJSON {
	return diffStatsJSON{FilesChanged: s.FilesChanged, Additions: s.Additions, Deletions: s.Deletions}
}

type singleBranchInfoJSON struct {
	Name             string              `json:"name"`
	IsCurrent        bool                `json:"is_current"`
	IsTrunk          bool                `json:"is_trunk"`
	IsLocked         bool                `json:"is_locked"`
	IsFrozen         bool                `json:"is_frozen"`
	NeedsRestack     bool                `json:"needs_restack"`
	Scope            string              `json:"scope"`
	CommitDate       string              `json:"commit_date,omitempty"`
	Parent           string              `json:"parent,omitempty"`
	Children         []string            `json:"children,omitempty"`
	PR               *singleBranchPRJSON `json:"pr,omitempty"`
	CommitMessages   []string            `json:"commit_messages"`
	DiffStats        diffStatsJSON       `json:"diff_stats"`
	StackTitle       string              `json:"stack_title,omitempty"`
	StackDescription string              `json:"stack_description,omitempty"`
}

type singleBranchPRJSON struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	IsDraft bool   `json:"is_draft"`
	URL     string `json:"url"`
}

type branchInfoRenderOptions struct {
	Body bool
}

func renderBranchInfoText(info actions.BranchInfoResult, opts branchInfoRenderOptions) string {
	var outputLines []string

	coloredBranchName := style.ColorBranchNameWithTrunk(info.Name, style.BranchStyleOpts{IsCurrent: info.IsCurrent, IsTrunk: info.IsTrunk})
	if info.IsLocked {
		coloredBranchName += " " + style.IconLocked() + " " + style.ColorDim("(locked)")
	}
	if info.IsFrozen {
		coloredBranchName += " " + style.IconFrozen() + " " + style.ColorDim("(frozen)")
	}
	if info.NeedsRestack {
		coloredBranchName += " " + style.ColorNeedsRestack("(needs restack)")
	}
	if info.Scope != "" {
		coloredBranchName += " " + style.ColorScope(info.Scope)
	}
	outputLines = append(outputLines, coloredBranchName)

	if info.StackTitle != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, style.RenderMarkdown(renderStackDescriptionMarkdown(info.StackTitle, info.StackDescription)))
	}

	if info.CommitDate != "" {
		outputLines = append(outputLines, style.ColorDim(info.CommitDate))
	}

	if prTitleLine := renderPRTitleLine(info.PR); prTitleLine != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, prTitleLine)
		if info.PR != nil && info.PR.URL != "" {
			outputLines = append(outputLines, style.ColorMagenta(info.PR.URL))
		}
	}

	isTrunkName := func(name string) bool { return info.Trunk != "" && name == info.Trunk }

	if info.Parent != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, fmt.Sprintf("%s: %s", style.ColorCyan("Parent"), style.ColorBranchNameWithTrunk(info.Parent, style.BranchStyleOpts{IsTrunk: isTrunkName(info.Parent)})))
	}

	if len(info.Children) > 0 {
		outputLines = append(outputLines, fmt.Sprintf("%s:", style.ColorCyan("Children")))
		for _, child := range info.Children {
			outputLines = append(outputLines, fmt.Sprintf("▸ %s", style.ColorBranchNameWithTrunk(child, style.BranchStyleOpts{IsTrunk: isTrunkName(child)})))
		}
	}

	if opts.Body && info.PR != nil && info.PR.Body != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, info.PR.Body)
	}

	outputLines = append(outputLines, "")
	if info.PatchOutput != "" {
		outputLines = append(outputLines, info.PatchOutput)
	} else {
		for _, commit := range info.CommitMessages {
			outputLines = append(outputLines, style.ColorDim(commit))
		}
	}

	if info.DiffOutput != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, info.DiffOutput)
	}

	if shouldDimBranchInfo(info.PR) {
		for i := range outputLines {
			outputLines[i] = style.ColorDim(outputLines[i])
		}
	}

	return strings.Join(outputLines, "\n")
}

func renderBranchInfoJSON(info actions.BranchInfoResult) ([]byte, error) {
	output := singleBranchInfoJSON{
		Name:             info.Name,
		IsCurrent:        info.IsCurrent,
		IsTrunk:          info.IsTrunk,
		IsLocked:         info.IsLocked,
		IsFrozen:         info.IsFrozen,
		NeedsRestack:     info.NeedsRestack,
		Scope:            info.Scope,
		CommitDate:       info.CommitDate,
		Parent:           info.Parent,
		Children:         info.Children,
		CommitMessages:   info.CommitMessages,
		DiffStats:        toDiffStatsJSON(info.DiffStats),
		StackTitle:       info.StackTitle,
		StackDescription: info.StackDescription,
	}

	if info.PR != nil && info.PR.Number != nil {
		output.PR = &singleBranchPRJSON{
			Number:  *info.PR.Number,
			Title:   info.PR.Title,
			State:   string(info.PR.State),
			IsDraft: info.PR.IsDraft,
			URL:     info.PR.URL,
		}
	}

	return json.MarshalIndent(output, "", "  ")
}

func renderPRTitleLine(prInfo *actions.BranchInfoPR) string {
	if prInfo == nil || prInfo.Number == nil || prInfo.Title == "" {
		return ""
	}

	prNumber := style.ColorPRNumber(*prInfo.Number)
	switch prInfo.State {
	case git.PRStateMerged:
		return fmt.Sprintf("%s (Merged) %s", prNumber, prInfo.Title)
	case git.PRStateClosed:
		return fmt.Sprintf("%s (Abandoned) %s", prNumber, style.ColorDim(prInfo.Title))
	default:
		prState := ""
		if prInfo.IsDraft {
			prState = style.ColorDim("(Draft)")
		}
		return fmt.Sprintf("%s %s %s", prNumber, prState, prInfo.Title)
	}
}

func renderStackDescriptionMarkdown(title, description string) string {
	if description != "" {
		return "# " + title + "\n\n" + description
	}
	return "# " + title
}

func shouldDimBranchInfo(prInfo *actions.BranchInfoPR) bool {
	if prInfo == nil {
		return false
	}
	return prInfo.State == git.PRStateMerged || prInfo.State == git.PRStateClosed
}

type stackInfoJSON struct {
	StackTitle       string            `json:"stack_title,omitempty"`
	StackDescription string            `json:"stack_description,omitempty"`
	Branches         []stackBranchJSON `json:"branches"`
}

type stackBranchJSON struct {
	Name           string        `json:"name"`
	Parent         string        `json:"parent"`
	IsLocked       bool          `json:"is_locked"`
	IsFrozen       bool          `json:"is_frozen"`
	Scope          string        `json:"scope"`
	PRNumber       *int          `json:"pr_number,omitempty"`
	PRURL          string        `json:"pr_url,omitempty"`
	CommitMessages []string      `json:"commit_messages"`
	DiffStats      diffStatsJSON `json:"diff_stats"`
}

func renderStackInfoJSON(result actions.StackInfoResult) ([]byte, error) {
	output := stackInfoJSON{
		StackTitle:       result.StackTitle,
		StackDescription: result.StackDescription,
		Branches:         make([]stackBranchJSON, 0, len(result.Branches)),
	}
	for _, b := range result.Branches {
		output.Branches = append(output.Branches, stackBranchJSON{
			Name:           b.Name,
			Parent:         b.Parent,
			IsLocked:       b.IsLocked,
			IsFrozen:       b.IsFrozen,
			Scope:          b.Scope,
			PRNumber:       b.PRNumber,
			PRURL:          b.PRURL,
			CommitMessages: b.CommitMessages,
			DiffStats:      toDiffStatsJSON(b.DiffStats),
		})
	}
	return json.MarshalIndent(output, "", "  ")
}

func renderStackInfoText(result actions.StackInfoResult) string {
	var sections []string

	if result.StackTitle != "" {
		markdown := renderStackDescriptionMarkdown(result.StackTitle, result.StackDescription)
		sections = append(sections, style.RenderMarkdown(markdown), "", strings.Repeat("─", 40), "")
	}

	stackTree := &tree.StackTree{
		Branches:       result.Order,
		CurrentBranchV: result.Current,
		TrunkBranch:    result.Trunk,
		ParentMap:      result.Parents,
		ChildrenMap:    childrenFromParents(result.Order, result.Parents),
		FixedMap:       result.UpToDate,
	}

	annotations := make(map[string]tree.BranchAnnotation, len(result.Branches))
	for _, b := range result.Branches {
		annotations[b.Name] = tree.BranchAnnotation{
			CommitMessages: b.CommitMessages,
			LinesAdded:     b.DiffStats.Additions,
			LinesDeleted:   b.DiffStats.Deletions,
			Scope:          b.Scope,
			IsLocked:       b.IsLocked,
			IsFrozen:       b.IsFrozen,
			PRNumber:       b.PRNumber,
			PRURL:          b.PRURL,
		}
	}

	renderer := tree.NewRenderer(stackTree)
	renderer.SetAnnotations(annotations)
	lines := renderer.RenderStack(result.Current, tree.RenderOptions{
		ShowCommitMessages:  true,
		HideSummary:         true,
		SkipSelectionPrefix: true,
	})

	sections = append(sections, strings.Join(lines, "\n"))
	return strings.Join(sections, "\n")
}

// childrenFromParents builds the inverse of a branch->parent map, preserving the
// order branches appear in, matching tree.NewStackTree's own construction.
func childrenFromParents(order []string, parents map[string]string) map[string][]string {
	children := make(map[string][]string)
	for _, name := range order {
		if parent := parents[name]; parent != "" {
			children[parent] = append(children[parent], name)
		}
	}
	return children
}
