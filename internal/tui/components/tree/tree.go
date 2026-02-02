// Package tree provides a renderer for branch tree visualizations.
package tree

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"stackit.dev/stackit/internal/tui/style"
)

const (
	// CurrentBranchSymbol is the symbol used for the current branch in tree views
	CurrentBranchSymbol = "◉"
	// BranchSymbol is the symbol used for regular branches in tree views
	BranchSymbol = "◯"

	// PRStateMerged indicates the PR has been merged
	PRStateMerged = "MERGED"
	// PRStateClosed indicates the PR has been closed
	PRStateClosed = "CLOSED"

	// CheckStatusNone indicates no CI status
	CheckStatusNone = "NONE"
	// CheckStatusPassing indicates CI is passing
	CheckStatusPassing = "PASSING"
	// CheckStatusFailing indicates CI is failing
	CheckStatusFailing = "FAILING"
	// CheckStatusPending indicates CI is pending
	CheckStatusPending = "PENDING"
)

// MergedParentDisplay represents a historical merged parent for display purposes
type MergedParentDisplay struct {
	BranchName string
	PRNumber   *int
	PRState    string // "MERGED", "CLOSED"
}

// BranchAnnotation holds per-branch display metadata
type BranchAnnotation struct {
	PRNumber      *int
	PRAction      string // "create", "update", "skip", ""
	CheckStatus   string // PASSING, FAILING, PENDING, NONE, ""
	ReviewStatus  string // "Approved", "In Review", "Changes Requested", "Commented", ""
	IsDraft       bool
	IsLocked      bool
	IsFrozen      bool
	NeedsRestack  bool
	CustomLabel   string // Additional text to display after branch name
	Scope         string
	ExplicitScope string

	CommitCount  int
	LinesAdded   int
	LinesDeleted int
	PRState      string // "OPEN", "MERGED", "CLOSED"

	// WorktreePath is set if this branch is the stack root of a managed worktree
	WorktreePath string

	// IsEmptyWorktree indicates this is a worktree anchor with no child branches
	IsEmptyWorktree bool

	// LocalSHA is the short commit SHA of the branch head (for debugging/diagnostics)
	LocalSHA string

	// MergedDownstack holds historical merged parents for display
	MergedDownstack []MergedParentDisplay

	// CommitMessages holds formatted commit messages (e.g., "abc123 Commit message")
	// Used when ShowCommitMessages is enabled in RenderOptions
	CommitMessages []string

	// PRURL is the GitHub PR URL for this branch
	PRURL string
}

// RenderMode specifies the rendering style for the tree.
type RenderMode int

const (
	// RenderModeFull shows multi-line branches with stats and summary.
	// Each branch takes multiple lines: branch info line, summary line, spacer.
	RenderModeFull RenderMode = iota

	// RenderModeCompact shows single-line branches with minimal info.
	// Good for quick overviews and narrower terminal widths.
	RenderModeCompact

	// RenderModeSelect shows single-line branches optimized for selection UI.
	// Similar to Compact but includes selection-related features.
	RenderModeSelect
)

// RenderOptions configures rendering behavior
type RenderOptions struct {
	// Mode specifies the rendering style. Defaults to RenderModeFull.
	Mode RenderMode

	// Steps limits traversal depth in each direction.
	Steps *int

	// OmitCurrentBranch hides the current branch from the tree.
	OmitCurrentBranch bool

	// NoStyleBranchName disables styling on branch names.
	NoStyleBranchName bool

	// HideStats hides commit count and line change stats.
	HideStats bool

	// HideSummary hides the entire summary line (stats, PR info, CI status).
	HideSummary bool

	// ShowSHAs shows commit SHAs next to branch names (for debugging).
	ShowSHAs bool

	// ShowCommitMessages shows commit messages below each branch.
	// Uses CommitMessages from BranchAnnotation.
	ShowCommitMessages bool

	// SelectedBranch is the name of the currently selected branch (for cursor).
	SelectedBranch string

	// Collapsed maps branch names to whether they are collapsed.
	Collapsed map[string]bool

	// SearchQuery is the current search filter text.
	SearchQuery string

	// SearchMatches maps branch names to whether they match the search.
	SearchMatches map[string]bool

	// NonSelectable marks branches that are visible but not selectable.
	NonSelectable map[string]bool

	// SkipSelectionPrefix omits the selection cursor/padding prefix.
	SkipSelectionPrefix bool
}

// isShortMode returns true if the options indicate short/compact rendering.
func (o RenderOptions) isShortMode() bool {
	return o.Mode == RenderModeCompact
}

// isSingleLineMode returns true if the options indicate single-line rendering.
func (o RenderOptions) isSingleLineMode() bool {
	return o.Mode == RenderModeSelect
}

// Data provides the tree structure data for rendering.
// This interface abstracts the source of tree data, making the renderer
// more testable and decoupled from specific implementations.
type Data interface {
	// CurrentBranch returns the name of the currently checked out branch
	CurrentBranch() string
	// Trunk returns the name of the trunk/main branch
	Trunk() string
	// Children returns the child branches of the given branch
	Children(branchName string) []string
	// Parent returns the parent branch of the given branch
	Parent(branchName string) string
	// IsTrunk returns whether the given branch is a trunk branch
	IsTrunk(branchName string) bool
	// IsFixed returns whether the branch is up-to-date with its parent
	IsFixed(branchName string) bool
}

// StackTreeRenderer renders branch trees with annotations
type StackTreeRenderer struct {
	currentBranch string
	trunk         string
	getChildren   func(branchName string) []string
	getParent     func(branchName string) string
	isTrunk       func(branchName string) bool
	isBranchFixed func(branchName string) bool
	Annotations   map[string]BranchAnnotation

	// childrenCache memoizes getChildren calls during rendering.
	// This is lazily populated and cleared between render operations.
	childrenCache map[string][]string

	// indentCache caches pre-computed indent strings to avoid repeated strings.Repeat calls.
	// Key is indent level, value is the pre-computed string like "│ │ │ ".
	// This cache persists across renders as indent strings are deterministic.
	indentCache map[int]string

	// styleCache caches lipgloss styles by key to avoid repeated NewStyle() calls.
	// Keys use format: "scope:<name>" for scope colors, "dim" for dim style, etc.
	// This cache persists across renders as styles are deterministic.
	styleCache map[string]lipgloss.Style
}

// NewRenderer creates a new tree renderer from a Data source.
// This is the preferred constructor as it uses a clean interface.
func NewRenderer(data Data) *StackTreeRenderer {
	return &StackTreeRenderer{
		currentBranch: data.CurrentBranch(),
		trunk:         data.Trunk(),
		getChildren:   data.Children,
		getParent:     data.Parent,
		isTrunk:       data.IsTrunk,
		isBranchFixed: data.IsFixed,
		Annotations:   make(map[string]BranchAnnotation),
	}
}

// SetAnnotation sets the annotation for a branch
func (r *StackTreeRenderer) SetAnnotation(branchName string, annotation BranchAnnotation) {
	r.Annotations[branchName] = annotation
}

// SetAnnotations sets annotations for multiple branches
func (r *StackTreeRenderer) SetAnnotations(annotations map[string]BranchAnnotation) {
	r.Annotations = annotations
}

// children returns the children of a branch, using the cache if available.
// The cache is populated on first access and reused for subsequent calls.
func (r *StackTreeRenderer) children(branchName string) []string {
	if r.childrenCache != nil {
		if cached, ok := r.childrenCache[branchName]; ok {
			return cached
		}
	}
	result := r.getChildren(branchName)
	if r.childrenCache != nil {
		r.childrenCache[branchName] = result
	}
	return result
}

// initCache initializes the caches for a new render operation.
func (r *StackTreeRenderer) initCache() {
	// Only create childrenCache per-render; indent and style caches persist
	r.childrenCache = make(map[string][]string)
	if r.indentCache == nil {
		r.indentCache = make(map[int]string)
	}
	if r.styleCache == nil {
		r.styleCache = make(map[string]lipgloss.Style)
	}
}

// clearCache clears the per-render caches after a render operation.
// Indent and style caches are preserved as they are deterministic.
func (r *StackTreeRenderer) clearCache() {
	r.childrenCache = nil
	// Note: indentCache and styleCache are preserved across renders
}

// getIndentString returns a cached indent string for the given level.
// This avoids repeated strings.Repeat calls which allocate new strings each time.
func (r *StackTreeRenderer) getIndentString(level int) string {
	if r.indentCache == nil {
		r.indentCache = make(map[int]string)
	}
	if s, ok := r.indentCache[level]; ok {
		return s
	}
	s := strings.Repeat("│ ", level)
	r.indentCache[level] = s
	return s
}

// getCachedStyle returns a cached lipgloss style for the given key.
// If the style doesn't exist, it calls builder to create it and caches the result.
func (r *StackTreeRenderer) getCachedStyle(key string, builder func() lipgloss.Style) lipgloss.Style {
	if r.styleCache == nil {
		r.styleCache = make(map[string]lipgloss.Style)
	}
	if s, ok := r.styleCache[key]; ok {
		return s
	}
	s := builder()
	r.styleCache[key] = s
	return s
}

// getScopeStyle returns a cached style for the given scope color.
func (r *StackTreeRenderer) getScopeStyle(scope string) lipgloss.Style {
	if scope == "" {
		return lipgloss.NewStyle()
	}
	return r.getCachedStyle("scope:"+scope, func() lipgloss.Style {
		if color, ok := style.GetScopeColor(scope); ok {
			return lipgloss.NewStyle().Foreground(color)
		}
		return lipgloss.NewStyle()
	})
}

// getDimStyle returns a cached dim style.
func (r *StackTreeRenderer) getDimStyle() lipgloss.Style {
	return r.getCachedStyle("dim", func() lipgloss.Style {
		return lipgloss.NewStyle().Foreground(style.DimColor())
	})
}

// RenderedBranch represents a branch and its rendered lines
type RenderedBranch struct {
	Name  string
	Lines []string
	// CursorLineIndex is the index within Lines where the selection cursor should appear (typically 0)
	CursorLineIndex int
	// CursorLineSelected is the pre-computed cursor line with selection styling.
	// This is populated during render to enable fast selection updates without re-rendering.
	CursorLineSelected string
}

// CachedBranchRender holds pre-rendered branch content with both selected and unselected versions.
// This allows fast updates when only the selection changes.
type CachedBranchRender struct {
	Name               string   // Branch name
	LinesUnselected    []string // Lines without selection (padding prefix, no highlight)
	CursorLineSelected string   // The cursor line with selection styling (cursor prefix + highlighted name)
	CursorLineIndex    int      // Which line gets the cursor
	IsNonSelectable    bool     // If true, never show as selected
}

// CachedTreeData holds the entire cached tree structure for fast selection updates.
type CachedTreeData struct {
	Branches []CachedBranchRender
}

// ApplySelection applies selection styling to cached branch data.
// This is fast because it only swaps the cursor line between selected/unselected versions.
func (c *CachedTreeData) ApplySelection(selectedBranch string) []RenderedBranch {
	result := make([]RenderedBranch, len(c.Branches))

	for i, branch := range c.Branches {
		isSelected := branch.Name == selectedBranch && !branch.IsNonSelectable

		// Copy lines, swapping the cursor line if selected
		lines := make([]string, len(branch.LinesUnselected))
		copy(lines, branch.LinesUnselected)

		if isSelected && branch.CursorLineIndex < len(lines) && branch.CursorLineSelected != "" {
			lines[branch.CursorLineIndex] = branch.CursorLineSelected
		}

		result[i] = RenderedBranch{
			Name:            branch.Name,
			Lines:           lines,
			CursorLineIndex: branch.CursorLineIndex,
		}
	}

	return result
}

// RenderStackCached renders the tree and returns cached data for fast selection updates.
// It renders once, generating both selected and unselected cursor lines in a single pass.
func (r *StackTreeRenderer) RenderStackCached(branchName string, opts RenderOptions) *CachedTreeData {
	// Render the tree without any selection - each branch will have its
	// CursorLineSelected pre-computed during the render
	optsNoSel := opts
	optsNoSel.SelectedBranch = ""
	branches := r.RenderStackDetailed(branchName, optsNoSel)

	// Build the cached data using the pre-computed values
	cached := &CachedTreeData{
		Branches: make([]CachedBranchRender, len(branches)),
	}

	for i, b := range branches {
		isNonSel := opts.NonSelectable != nil && opts.NonSelectable[b.Name]
		cached.Branches[i] = CachedBranchRender{
			Name:               b.Name,
			LinesUnselected:    b.Lines,
			CursorLineIndex:    b.CursorLineIndex,
			IsNonSelectable:    isNonSel,
			CursorLineSelected: b.CursorLineSelected, // Pre-computed during render
		}
	}

	return cached
}

// RenderStack renders the full stack tree starting from a branch
func (r *StackTreeRenderer) RenderStack(branchName string, opts RenderOptions) []string {
	rendered := r.RenderStackDetailed(branchName, opts)
	// Pre-allocate capacity based on total lines across all branches
	totalLines := 0
	for _, b := range rendered {
		totalLines += len(b.Lines)
	}
	result := make([]string, 0, totalLines)
	for _, b := range rendered {
		result = append(result, b.Lines...)
	}

	// Apply short formatting if needed (handles both Mode and legacy Short field)
	if opts.isShortMode() {
		return r.formatShortLines(result, treeRenderArgs{
			short:             true,
			noStyleBranchName: opts.NoStyleBranchName,
			currentBranch:     r.currentBranch,
			overallIndent:     nil, // formatShortLines will recalculate if needed
		})
	}

	return result
}

// RenderStackDetailed renders the full stack tree and returns detailed branch info
func (r *StackTreeRenderer) RenderStackDetailed(branchName string, opts RenderOptions) []RenderedBranch {
	// Initialize cache for this render operation
	r.initCache()
	defer r.clearCache()

	overallIndent := 0
	args := treeRenderArgs{
		// Config fields (use helper methods to handle Mode and legacy flags)
		short:               opts.isShortMode(),
		singleLine:          opts.isSingleLineMode(),
		omitCurrentBranch:   opts.OmitCurrentBranch,
		noStyleBranchName:   opts.NoStyleBranchName,
		hideStats:           opts.HideStats,
		hideSummary:         opts.HideSummary,
		showSHAs:            opts.ShowSHAs,
		showCommitMessages:  opts.ShowCommitMessages,
		skipSelectionPrefix: opts.SkipSelectionPrefix,
		selectedBranch:      opts.SelectedBranch,
		currentBranch:       r.currentBranch,
		collapsed:           opts.Collapsed,
		searchQuery:         opts.SearchQuery,
		searchMatches:       opts.SearchMatches,
		nonSelectable:       opts.NonSelectable,
		// Traversal fields
		branchName:   branchName,
		indentLevel:  0,
		parentScopes: []string{},
		steps:        opts.Steps,
		// State fields
		visited:       make(map[string]bool),
		overallIndent: &overallIndent,
	}

	outputDeep := [][]RenderedBranch{
		r.getUpstackExclusiveRendered(args),
		r.getBranchRendered(args),
		r.getDownstackExclusiveRendered(args),
	}

	// Flatten with pre-allocated capacity
	totalLen := 0
	for _, section := range outputDeep {
		totalLen += len(section)
	}
	result := make([]RenderedBranch, 0, totalLen)
	for _, section := range outputDeep {
		result = append(result, section...)
	}

	return result
}

// treeRenderArgs holds all arguments for tree rendering.
// Fields are organized into logical groups:
// - Config fields (constant during render): short, singleLine, reverse, etc.
// - Traversal fields (change per branch): branchName, indentLevel, parentScopes, etc.
// - State fields (shared mutable): visited, overallIndent
type treeRenderArgs struct {
	// Config fields - constant during entire render
	short               bool
	singleLine          bool
	omitCurrentBranch   bool
	noStyleBranchName   bool
	hideStats           bool
	hideSummary         bool
	showSHAs            bool
	showCommitMessages  bool
	skipSelectionPrefix bool
	selectedBranch      string
	currentBranch       string
	collapsed           map[string]bool
	searchQuery         string
	searchMatches       map[string]bool
	nonSelectable       map[string]bool

	// Traversal fields - change per branch during traversal
	branchName        string
	indentLevel       int
	parentScopes      []string
	steps             *int
	skipBranchingLine bool

	// State fields - shared mutable state (pointers for sharing)
	visited       map[string]bool
	overallIndent *int
}

// childArgs creates a new treeRenderArgs for a child branch, inheriting config and state.
// This avoids copying all config fields manually when recursing.
func (a treeRenderArgs) childArgs(branchName string, indentLevel int, parentScopes []string, steps *int) treeRenderArgs {
	return treeRenderArgs{
		// Config fields (inherited)
		short:               a.short,
		singleLine:          a.singleLine,
		omitCurrentBranch:   a.omitCurrentBranch,
		noStyleBranchName:   a.noStyleBranchName,
		hideStats:           a.hideStats,
		hideSummary:         a.hideSummary,
		showSHAs:            a.showSHAs,
		showCommitMessages:  a.showCommitMessages,
		skipSelectionPrefix: a.skipSelectionPrefix,
		selectedBranch:      a.selectedBranch,
		currentBranch:       a.currentBranch,
		collapsed:           a.collapsed,
		searchQuery:         a.searchQuery,
		searchMatches:       a.searchMatches,
		nonSelectable:       a.nonSelectable,
		// Traversal fields (new for this branch)
		branchName:        branchName,
		indentLevel:       indentLevel,
		parentScopes:      parentScopes,
		steps:             steps,
		skipBranchingLine: false,
		// State fields (shared)
		visited:       a.visited,
		overallIndent: a.overallIndent,
	}
}

// downstackArgs creates args for downstack rendering (with skipBranchingLine=true).
func (a treeRenderArgs) downstackArgs(branchName string) treeRenderArgs {
	return treeRenderArgs{
		// Config fields (inherited)
		short:               a.short,
		singleLine:          a.singleLine,
		omitCurrentBranch:   a.omitCurrentBranch,
		noStyleBranchName:   a.noStyleBranchName,
		hideStats:           a.hideStats,
		hideSummary:         a.hideSummary,
		showSHAs:            a.showSHAs,
		showCommitMessages:  a.showCommitMessages,
		skipSelectionPrefix: a.skipSelectionPrefix,
		selectedBranch:      a.selectedBranch,
		currentBranch:       a.currentBranch,
		collapsed:           a.collapsed,
		searchQuery:         a.searchQuery,
		searchMatches:       a.searchMatches,
		nonSelectable:       a.nonSelectable,
		// Traversal fields (for downstack branch)
		branchName:        branchName,
		indentLevel:       a.indentLevel,
		parentScopes:      a.parentScopes,
		skipBranchingLine: true,
		// State fields (shared)
		visited:       a.visited,
		overallIndent: a.overallIndent,
	}
}

func (r *StackTreeRenderer) getUpstackExclusiveRendered(args treeRenderArgs) []RenderedBranch {
	if args.steps != nil && *args.steps == 0 {
		return []RenderedBranch{}
	}

	if args.collapsed != nil && args.collapsed[args.branchName] {
		return []RenderedBranch{}
	}

	if args.visited == nil {
		args.visited = make(map[string]bool)
	}
	if args.visited[args.branchName] {
		return []RenderedBranch{}
	}
	args.visited[args.branchName] = true
	defer func() { delete(args.visited, args.branchName) }()

	children := r.children(args.branchName)
	filteredChildren := []string{}
	for _, child := range children {
		if !args.omitCurrentBranch || child != r.currentBranch {
			filteredChildren = append(filteredChildren, child)
		}
	}

	var result []RenderedBranch
	for i, child := range filteredChildren {
		childSteps := args.steps
		if childSteps != nil {
			nextStep := *childSteps - 1
			childSteps = &nextStep
		}

		childIndent := args.indentLevel + i

		childParentScopes := append([]string{}, args.parentScopes...)
		parentScope := r.Annotations[args.branchName].Scope
		for len(childParentScopes) < childIndent {
			childParentScopes = append(childParentScopes, parentScope)
		}

		childBranches := r.getUpstackInclusiveRendered(args.childArgs(child, childIndent, childParentScopes, childSteps))
		result = append(result, childBranches...)
	}

	return result
}

func (r *StackTreeRenderer) getUpstackInclusiveRendered(args treeRenderArgs) []RenderedBranch {
	upstack := r.getUpstackExclusiveRendered(args)
	current := r.getBranchRendered(args)

	outputDeep := [][]RenderedBranch{upstack, current}

	// Pre-allocate with known capacity
	result := make([]RenderedBranch, 0, len(upstack)+len(current))
	for _, section := range outputDeep {
		result = append(result, section...)
	}
	return result
}

func (r *StackTreeRenderer) getDownstackExclusiveRendered(args treeRenderArgs) []RenderedBranch {
	if r.isTrunk(args.branchName) {
		return []RenderedBranch{}
	}

	// Build stack in reverse order (parent to grandparent), then reverse once
	// This avoids O(n²) behavior from prepending to a slice
	var fullStack []string
	current := args.branchName
	visited := make(map[string]bool)
	for {
		parent := r.getParent(current)
		if parent == "" || r.isTrunk(parent) || visited[parent] {
			break
		}
		fullStack = append(fullStack, parent)
		current = parent
		visited[current] = true
	}
	// Reverse to get correct order (grandparent to parent)
	for i, j := 0, len(fullStack)-1; i < j; i, j = i+1, j-1 {
		fullStack[i], fullStack[j] = fullStack[j], fullStack[i]
	}
	// Prepend trunk (single allocation)
	fullStack = append([]string{r.trunk}, fullStack...)

	if args.steps != nil && *args.steps > 0 {
		start := len(fullStack) - *args.steps
		if start < 0 {
			start = 0
		}
		fullStack = fullStack[start:]
	}

	var result []RenderedBranch
	for _, branchName := range fullStack {
		branchData := r.getBranchRendered(args.downstackArgs(branchName))
		result = append(result, branchData...)
	}

	// Reverse to get trunk at bottom
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

func (r *StackTreeRenderer) getBranchRendered(args treeRenderArgs) []RenderedBranch {
	lines, cursorIdx := r.getBranchLinesWithCursor(args)

	// Pre-compute the selected cursor line for fast selection updates.
	// Skip if branch is non-selectable or if selection prefix is skipped.
	var cursorLineSelected string
	isNonSelectable := args.nonSelectable != nil && args.nonSelectable[args.branchName]
	if !args.skipSelectionPrefix && !isNonSelectable {
		cursorLineSelected = r.renderCursorLineSelected(args, cursorIdx)
	}

	return []RenderedBranch{{
		Name:               args.branchName,
		Lines:              lines,
		CursorLineIndex:    cursorIdx,
		CursorLineSelected: cursorLineSelected,
	}}
}

// getBranchLinesWithCursor returns the rendered lines and the index of the cursor line
func (r *StackTreeRenderer) getBranchLinesWithCursor(args treeRenderArgs) ([]string, int) {
	lines := r.getBranchLines(args)

	// In short format, there's only one line (the cursor line)
	if args.short {
		return lines, 0
	}

	// In full format, determine cursor position
	children := r.children(args.branchName)
	hasBranchingLine := !args.skipBranchingLine && len(children) >= 2
	cursorIdx := 0
	if hasBranchingLine {
		cursorIdx = 1 // Cursor is on the info line after the branching line
	}

	return lines, cursorIdx
}

// renderCursorLineSelected generates the cursor line with selection styling applied.
// This is used to pre-compute selected cursor lines for fast selection updates.
func (r *StackTreeRenderer) renderCursorLineSelected(args treeRenderArgs, cursorIdx int) string {
	// Create modified args with this branch selected
	selectedArgs := args
	selectedArgs.selectedBranch = args.branchName

	// Generate lines with selection applied
	selectedLines := r.getBranchLines(selectedArgs)

	// Return the cursor line from the selected render
	if cursorIdx < len(selectedLines) {
		return selectedLines[cursorIdx]
	}
	return ""
}

func (r *StackTreeRenderer) getBranchLines(args treeRenderArgs) []string {
	children := r.children(args.branchName)
	numChildren := len(children)

	if args.overallIndent != nil {
		if args.indentLevel > *args.overallIndent {
			*args.overallIndent = args.indentLevel
		}
	}

	// Short format
	if args.short {
		// Check if branch is non-selectable
		isNonSelectable := args.nonSelectable != nil && args.nonSelectable[args.branchName]

		// Selection cursor prefix (non-selectable branches never show cursor)
		cursorPrefix := ""
		if !args.skipSelectionPrefix {
			isSelected := args.branchName == args.selectedBranch
			cursorPrefix = style.SelectionPadding
			if isSelected && !isNonSelectable {
				cursorPrefix = style.SelectionCursorStyle().Render(style.SelectionCursor)
			}
		}

		// Build the line using strings.Builder for efficiency
		var b strings.Builder
		// Pre-allocate: cursor(3) + indent(3*level) + branch(6) + name(~30) + annotations(~50)
		b.Grow(100 + args.indentLevel*3)

		b.WriteString(cursorPrefix)
		b.WriteString(r.getIndentString(args.indentLevel))

		// Add branching characters
		if !args.skipBranchingLine && numChildren > 1 {
			b.WriteString(strings.Repeat("─┴", numChildren-2))
			b.WriteString("─┘")
		} else if !args.skipBranchingLine && numChildren == 1 {
			b.WriteString("─┘")
		}

		// Add circle and branch name
		isCurrent := args.branchName == r.currentBranch
		if isCurrent && !args.noStyleBranchName {
			b.WriteString(CurrentBranchSymbol)
		} else {
			b.WriteString(BranchSymbol)
		}

		// Dim the branch name if non-selectable
		branchNameStr := args.branchName
		if isNonSelectable {
			branchNameStr = style.ColorDim(branchNameStr)
		}
		b.WriteString("▸")
		b.WriteString(branchNameStr)

		// Add annotation
		annotation := r.Annotations[args.branchName]
		b.WriteString(r.formatAnnotation(annotation, args.noStyleBranchName))

		// Add empty worktree indicator
		if annotation.IsEmptyWorktree {
			b.WriteString(" ")
			b.WriteString(style.ColorDim("<empty>"))
		}
		// Add worktree indicator
		if annotation.WorktreePath != "" {
			b.WriteString(" ")
			b.WriteString(style.ColorDim("📂 worktree"))
		}

		// Add restack indicator
		if !args.noStyleBranchName && !r.isBranchFixed(args.branchName) {
			b.WriteString(" (needs restack)")
		}

		return []string{b.String()}
	}

	// Full format
	var result []string

	// Branching line
	if !args.skipBranchingLine && numChildren >= 2 {
		result = append(result, r.getBranchingLine(numChildren, args.indentLevel, args.parentScopes, args.branchName, args.skipSelectionPrefix))
	}

	// Branch info lines
	infoLines := r.getInfoLines(args)
	result = append(result, infoLines...)

	return result
}

func (r *StackTreeRenderer) getBranchingLine(numChildren int, indentLevel int, parentScopes []string, branchName string, skipSelectionPrefix bool) string {
	if numChildren < 2 {
		return ""
	}

	var prefixBuilder strings.Builder
	for i := range indentLevel {
		scope := ""
		if i < len(parentScopes) {
			scope = parentScopes[i]
		}
		char := "│"
		if scope != "" {
			char = r.getScopeStyle(scope).Render(char)
		}
		prefixBuilder.WriteString(char + "  ")
	}
	prefix := prefixBuilder.String()

	// The branching characters connect the current branch to its children.
	// They should use the current branch's scope color.
	annotation := r.Annotations[branchName]
	scope := annotation.Scope
	isMerged := annotation.PRState == PRStateMerged
	isClosed := annotation.PRState == PRStateClosed
	isDim := isMerged || isClosed

	var styleObj lipgloss.Style
	switch {
	case isDim:
		styleObj = r.getDimStyle()
	case scope != "":
		styleObj = r.getScopeStyle(scope)
	default:
		styleObj = lipgloss.NewStyle()
	}

	middle := "──┴"
	last := "──┘"

	branchingChars := "├"
	if numChildren > 2 {
		branchingChars += strings.Repeat(middle, numChildren-2)
	}
	branchingChars += last

	selectionPrefix := ""
	if !skipSelectionPrefix {
		selectionPrefix = style.SelectionPadding
	}
	line := selectionPrefix + prefix + styleObj.Render(branchingChars)

	return line
}

func (r *StackTreeRenderer) getInfoLines(args treeRenderArgs) []string {
	isCurrent := args.branchName == r.currentBranch
	isSelected := args.branchName == args.selectedBranch
	annotation := r.Annotations[args.branchName]
	isTrunk := r.isTrunk(args.branchName)
	isMerged := annotation.PRState == PRStateMerged
	isClosed := annotation.PRState == PRStateClosed
	isDim := isMerged || isClosed

	// Check if branch matches search (if search is active)
	matchesSearch := true
	if args.searchMatches != nil {
		if match, ok := args.searchMatches[args.branchName]; ok {
			matchesSearch = match
		}
	}
	// Note: We don't set isDim for non-matching search results because we want
	// them to render in single-line mode, not the merged/closed 2-line format

	// Check if branch is non-selectable (visible but cursor skips it)
	isNonSelectable := args.nonSelectable != nil && args.nonSelectable[args.branchName]

	// Build prefix for indentation using cached styles
	var prefixBuilder strings.Builder
	for i := range args.indentLevel {
		scope := ""
		if i < len(args.parentScopes) {
			scope = args.parentScopes[i]
		}
		char := "│"
		if scope != "" {
			char = r.getScopeStyle(scope).Render(char)
		}
		prefixBuilder.WriteString(char + "  ")
	}
	prefix := prefixBuilder.String()

	// Symbol styling
	var symbol string
	if isCurrent {
		symbol = CurrentBranchSymbol
	} else {
		symbol = BranchSymbol
	}

	children := r.children(args.branchName)
	if len(children) > 0 {
		if args.collapsed != nil && args.collapsed[args.branchName] {
			symbol = "+"
		}
	}

	// Determine style based on branch state using cached styles
	var styleObj lipgloss.Style
	shouldDim := isDim || (!matchesSearch && args.searchQuery != "") || isNonSelectable
	switch {
	case shouldDim:
		styleObj = r.getDimStyle()
	case annotation.Scope != "":
		styleObj = r.getScopeStyle(annotation.Scope)
	default:
		styleObj = lipgloss.NewStyle()
	}

	// Parent style for connecting line
	parentScope := ""
	if parent := r.getParent(args.branchName); parent != "" {
		parentScope = r.Annotations[parent].Scope
	}
	var parentStyle lipgloss.Style
	switch {
	case isDim:
		parentStyle = r.getDimStyle()
	case parentScope != "":
		parentStyle = r.getScopeStyle(parentScope)
	default:
		parentStyle = lipgloss.NewStyle()
	}

	// Selection cursor prefix
	// Non-selectable branches never show cursor (even if they happen to be "selected")
	cursorPrefix := ""
	selectionPadding := ""
	if !args.skipSelectionPrefix {
		cursorPrefix = style.SelectionPadding // Default: spaces for alignment
		selectionPadding = style.SelectionPadding
		if isSelected && !isNonSelectable {
			cursorPrefix = style.SelectionCursorStyle().Render(style.SelectionCursor)
		}
	}

	// TRUNK: minimal single line
	if isTrunk {
		branchName := args.branchName
		coloredBranchName := style.BranchStyle(isCurrent, true, false).Render(branchName)
		if isSelected {
			coloredBranchName = style.Selection().Render(" " + branchName + " ")
		} else if !matchesSearch && args.searchQuery != "" {
			coloredBranchName = style.ColorDim(branchName)
		}
		return []string{cursorPrefix + prefix + styleObj.Render(symbol) + " " + coloredBranchName}
	}

	// MERGED/CLOSED: collapsed single line, dimmed
	if isDim {
		dimLine := cursorPrefix + prefix + styleObj.Render(symbol) + " " + style.ColorDim(args.branchName)
		if annotation.ExplicitScope != "" {
			dimLine += " " + style.ColorDim("["+annotation.ExplicitScope+"]")
		}
		// In single line mode, don't add trailing spacer
		if args.singleLine {
			return []string{dimLine}
		}
		return []string{
			dimLine,
			selectionPadding + prefix + parentStyle.Render("│"),
		}
	}

	var result []string

	// LINE 1: Symbol + Branch Name (bold if current) + SHA + Scope + Actionable Warnings
	branchName := args.branchName
	coloredBranchName := style.ColorBranchNameBoldWithTrunk(branchName, isCurrent, isTrunk)

	switch {
	case isSelected && !isNonSelectable:
		coloredBranchName = style.Selection().Render(" " + branchName + " ")
	case isNonSelectable:
		// Gray out non-selectable branches
		coloredBranchName = style.ColorDim(branchName)
	case !matchesSearch && args.searchQuery != "":
		// Gray out non-matching branches
		coloredBranchName = style.ColorDim(branchName)
	}

	// Add SHA if requested (useful for debugging/diagnostics)
	if args.showSHAs && annotation.LocalSHA != "" {
		coloredBranchName += " " + style.ColorDim("(") + style.ColorSHA(annotation.LocalSHA) + style.ColorDim(")")
	}

	// Add scope (colored to match tree)
	if annotation.Scope != "" {
		coloredBranchName += " " + style.ColorScope(annotation.Scope)
	}

	// Actionable warnings only
	if !r.isBranchFixed(branchName) {
		coloredBranchName += " " + style.ColorNeedsRestack("(needs restack)")
	}
	if annotation.IsLocked {
		coloredBranchName += " " + style.IconLocked() + " " + style.ColorDim("(locked)")
	}
	if annotation.IsFrozen {
		coloredBranchName += " " + style.IconFrozen() + " " + style.ColorDim("(frozen)")
	}
	// Empty worktree indicator
	if annotation.IsEmptyWorktree {
		coloredBranchName += " " + style.ColorDim("<empty>")
	}
	// Worktree indicator (for stack roots with managed worktrees or empty worktree anchors)
	if annotation.WorktreePath != "" {
		coloredBranchName += " " + style.ColorDim("📂 worktree")
	}

	// Custom label (e.g., "<---- moving this branch" for move operation)
	if annotation.CustomLabel != "" {
		coloredBranchName += " " + style.ColorDim(annotation.CustomLabel)
	}

	result = append(result, cursorPrefix+prefix+styleObj.Render(symbol)+" "+coloredBranchName)

	// Show merged downstack history (most recent first)
	if !args.singleLine && !args.hideSummary && len(annotation.MergedDownstack) > 0 {
		branchPipe := styleObj.Render("│")
		for i := len(annotation.MergedDownstack) - 1; i >= 0; i-- {
			merged := annotation.MergedDownstack[i]
			historyLine := r.formatMergedParentLine(merged, prefix, branchPipe, selectionPadding)
			result = append(result, historyLine)
		}
	}

	if args.singleLine {
		// Add compact indicators for single-line mode
		compactSummary := r.formatCompactSummary(annotation, isTrunk)
		if compactSummary != "" {
			result[len(result)-1] += " " + compactSummary
		}
		return result
	}

	// Show commit messages when enabled (info mode)
	if args.showCommitMessages && len(annotation.CommitMessages) > 0 {
		branchPipe := styleObj.Render("│")
		// Show diff stats and PR info first (above commits)
		var statsLine string
		if annotation.LinesAdded > 0 || annotation.LinesDeleted > 0 {
			statsLine = r.formatContextualStats(annotation)
		}
		// Add PR info if available
		if annotation.PRNumber != nil {
			prInfo := style.ColorPRNumberByState(*annotation.PRNumber, annotation.PRState, annotation.IsDraft)
			if annotation.PRURL != "" {
				prInfo += " " + style.ColorDim(annotation.PRURL)
			}
			if statsLine != "" {
				statsLine += " " + style.ColorDim("|") + " " + prInfo
			} else {
				statsLine = prInfo
			}
		}
		if statsLine != "" {
			result = append(result, selectionPadding+prefix+branchPipe+"  "+statsLine)
		}
		// Then show commit messages
		for _, msg := range annotation.CommitMessages {
			// Color the SHA (first word), rest dimmed
			formattedMsg := msg
			if spaceIdx := strings.Index(msg, " "); spaceIdx > 0 {
				sha := msg[:spaceIdx]
				rest := msg[spaceIdx:]
				formattedMsg = style.ColorSHA(sha) + style.ColorDim(rest)
			} else {
				formattedMsg = style.ColorDim(msg)
			}
			result = append(result, selectionPadding+prefix+branchPipe+"  "+formattedMsg)
		}
	}

	// LINE 2: Summary line with PR# → Review → CI → Stats (skip if hideSummary or showCommitMessages)
	if !args.hideSummary && !args.showCommitMessages {
		branchPipe := styleObj.Render("│")
		summaryLine := r.formatSummaryLine(annotation, isTrunk, args.hideStats)

		if summaryLine != "" {
			result = append(result, selectionPadding+prefix+branchPipe+"  "+summaryLine)
		}
	}

	// Trailing spacer line
	result = append(result, selectionPadding+prefix+parentStyle.Render("│"))

	return result
}

// formatSummaryLine creates line 2: Stats | PR# CI Review | Action/Status
func (r *StackTreeRenderer) formatSummaryLine(annotation BranchAnnotation, isTrunk bool, hideStats bool) string {
	var prParts []string
	var statsParts []string
	var actionParts []string

	// PR number (colored by state)
	if annotation.PRNumber != nil {
		prParts = append(prParts, style.ColorPRNumberByState(*annotation.PRNumber, annotation.PRState, annotation.IsDraft))

		// Draft badge
		if annotation.IsDraft {
			prParts = append(prParts, style.ColorDim("Draft"))
		}
	}

	// CI status (colored dot)
	switch annotation.CheckStatus {
	case CheckStatusPassing:
		prParts = append(prParts, style.IconCIPassing())
	case CheckStatusFailing:
		prParts = append(prParts, style.IconCIFailing())
	case CheckStatusPending:
		prParts = append(prParts, style.IconCIPending())
	}

	// Review status icon
	switch annotation.ReviewStatus {
	case "Approved":
		prParts = append(prParts, style.IconReviewApproved())
	case "Changes Requested":
		prParts = append(prParts, style.IconReviewChangesRequested())
	}

	// Stats (contextual, already colored)
	if !isTrunk && !hideStats {
		stats := r.formatContextualStats(annotation)
		if stats != "" {
			statsParts = append(statsParts, stats)
		}
	}

	// PR Action (for submit view: create, update, skip)
	if annotation.PRAction != "" {
		actionParts = append(actionParts, style.ColorDim("→ "+annotation.PRAction))
	}

	// Custom label (for submit status: ✓, ✗, spinner)
	if annotation.CustomLabel != "" {
		actionParts = append(actionParts, annotation.CustomLabel)
	}

	// Join sections with pipe separators (stats first, then PR info)
	var result []string
	if len(statsParts) > 0 {
		result = append(result, strings.Join(statsParts, " "))
	}
	if len(prParts) > 0 {
		result = append(result, strings.Join(prParts, " "))
	}
	if len(actionParts) > 0 {
		result = append(result, strings.Join(actionParts, " "))
	}

	return strings.Join(result, " "+style.ColorDim("|")+" ")
}

// formatContextualStats shows stats only when meaningful
// - Commits: only if > 1
// - Lines: only if non-zero (green for adds, red for deletes)
func (r *StackTreeRenderer) formatContextualStats(annotation BranchAnnotation) string {
	var parts []string

	// Only show commit count if > 1
	if annotation.CommitCount > 1 {
		parts = append(parts, fmt.Sprintf("%dc", annotation.CommitCount))
	}

	// Only show lines if non-zero (colored: green for +, red for -)
	if annotation.LinesAdded > 0 || annotation.LinesDeleted > 0 {
		var lineParts []string
		if annotation.LinesAdded > 0 {
			lineParts = append(lineParts, style.ColorGreen(fmt.Sprintf("+%d", annotation.LinesAdded)))
		}
		if annotation.LinesDeleted > 0 {
			lineParts = append(lineParts, style.ColorRed(fmt.Sprintf("-%d", annotation.LinesDeleted)))
		}
		parts = append(parts, strings.Join(lineParts, "/"))
	}

	return strings.Join(parts, " ")
}

// formatMergedParentLine formats a single merged parent history line
func (r *StackTreeRenderer) formatMergedParentLine(
	merged MergedParentDisplay,
	prefix string,
	branchPipe string,
	selectionPadding string,
) string {
	var text string
	if merged.PRNumber != nil {
		prNum := style.ColorPRNumberByState(*merged.PRNumber, merged.PRState, false)
		text = fmt.Sprintf("previously based on %s", prNum)

		switch merged.PRState {
		case PRStateMerged:
			text += " " + style.ColorDim("(merged)")
		case PRStateClosed:
			text += " " + style.ColorDim("(closed)")
		}
	} else {
		text = fmt.Sprintf("previously based on %s", style.ColorDim(merged.BranchName))
	}

	return selectionPadding + prefix + branchPipe + "  " + style.ColorDim(text)
}

// formatCompactSummary returns a compact summary for single-line mode
// Shows: PR# CI-icon (e.g., "#123 ●")
func (r *StackTreeRenderer) formatCompactSummary(annotation BranchAnnotation, isTrunk bool) string {
	if isTrunk {
		return ""
	}

	var parts []string

	// PR number (dimmed)
	if annotation.PRNumber != nil {
		parts = append(parts, style.ColorDim(fmt.Sprintf("#%d", *annotation.PRNumber)))
	}

	// CI status icon
	switch annotation.CheckStatus {
	case CheckStatusPassing:
		parts = append(parts, style.IconCIPassing())
	case CheckStatusFailing:
		parts = append(parts, style.IconCIFailing())
	case CheckStatusPending:
		parts = append(parts, style.IconCIPending())
	}

	return strings.Join(parts, " ")
}

func (r *StackTreeRenderer) formatAnnotation(annotation BranchAnnotation, _ bool) string {
	var parts []string

	if annotation.PRNumber != nil {
		parts = append(parts, formatPRNumberPlain(*annotation.PRNumber))
	}

	if annotation.Scope != "" {
		parts = append(parts, "["+annotation.Scope+"]")
	}

	if annotation.PRAction != "" {
		parts = append(parts, annotation.PRAction)
	}

	if annotation.CheckStatus != "" && annotation.CheckStatus != CheckStatusNone {
		icon := r.checksIcon(annotation.CheckStatus)
		parts = append(parts, icon)
	}

	if annotation.IsDraft {
		parts = append(parts, "(Draft)")
	}

	if annotation.IsLocked {
		parts = append(parts, style.IconLocked()+" "+style.ColorDim("(locked)"))
	}

	if annotation.IsFrozen {
		parts = append(parts, style.IconFrozen()+" "+style.ColorDim("(frozen)"))
	}

	if annotation.CustomLabel != "" {
		parts = append(parts, annotation.CustomLabel)
	}

	if len(parts) == 0 {
		return ""
	}

	return " " + strings.Join(parts, " ")
}

// FormatAnnotationColored returns a colored string representation of a branch annotation
func (r *StackTreeRenderer) FormatAnnotationColored(annotation BranchAnnotation) string {
	var parts []string

	// PR number (colored by state)
	if annotation.PRNumber != nil {
		parts = append(parts, style.ColorPRNumberByState(*annotation.PRNumber, annotation.PRState, annotation.IsDraft))
	}

	if annotation.Scope != "" {
		parts = append(parts, style.ColorScope(annotation.Scope))
	}

	if annotation.PRAction != "" {
		parts = append(parts, style.ColorDim("→ "+annotation.PRAction))
	}

	if annotation.CheckStatus != "" && annotation.CheckStatus != CheckStatusNone {
		icon := r.checksIcon(annotation.CheckStatus)
		switch annotation.CheckStatus {
		case CheckStatusPassing:
			parts = append(parts, style.ColorCyan(icon))
		case CheckStatusFailing:
			parts = append(parts, style.ColorRed(icon))
		case CheckStatusPending:
			parts = append(parts, style.ColorYellow(icon))
		default:
			parts = append(parts, icon)
		}
	}

	if annotation.IsDraft {
		parts = append(parts, style.ColorDim("(Draft)"))
	}

	if annotation.IsLocked {
		parts = append(parts, style.IconLocked()+" "+style.ColorDim("(locked)"))
	}
	if annotation.IsFrozen {
		parts = append(parts, style.IconFrozen()+" "+style.ColorDim("(frozen)"))
	}

	if annotation.PRState == PRStateMerged {
		parts = append(parts, style.ColorDim("(Merged)"))
	} else if annotation.PRState == PRStateClosed {
		parts = append(parts, style.ColorDim("(Closed)"))
	}

	if annotation.CustomLabel != "" {
		parts = append(parts, style.ColorDim(annotation.CustomLabel))
	}

	if len(parts) == 0 {
		return ""
	}

	return " " + strings.Join(parts, " ")
}

func (r *StackTreeRenderer) checksIcon(status string) string {
	switch status {
	case CheckStatusPassing:
		return "✓"
	case CheckStatusFailing:
		return "✗"
	case CheckStatusPending:
		return "⏳"
	default:
		return ""
	}
}

func formatPRNumberPlain(prNumber int) string {
	return "#" + strings.TrimPrefix(style.ColorPRNumber(prNumber), "PR ")
}

func (r *StackTreeRenderer) formatShortLines(lines []string, args treeRenderArgs) []string {
	var result []string

	for _, line := range lines {
		circleIndex := strings.Index(line, BranchSymbol)
		arrowIndex := strings.Index(line, "▸")

		if circleIndex == -1 {
			circleIndex = strings.Index(line, CurrentBranchSymbol)
		}

		if circleIndex != -1 && arrowIndex != -1 {
			// Extract branch name to check if it's current
			// arrowIndex is a byte index, need to skip full UTF-8 character
			arrowRune := '▸'
			arrowWidth := utf8.RuneLen(arrowRune)
			branchNameAndDetails := line[arrowIndex+arrowWidth:]
			branchName := strings.Fields(branchNameAndDetails)[0]
			isCurrent := !args.noStyleBranchName && args.currentBranch != "" && branchName == args.currentBranch

			overallIndent := 0
			if args.overallIndent != nil {
				overallIndent = *args.overallIndent
			}

			formatted := style.FormatShortLine(line, circleIndex, arrowIndex, isCurrent, overallIndent)
			result = append(result, formatted)
		} else {
			result = append(result, line)
		}
	}

	return result
}

// RenderBranchList renders a simple list of branches with annotations (no tree structure)
func (r *StackTreeRenderer) RenderBranchList(branches []string) []string {
	result := make([]string, 0, len(branches))

	for _, branchName := range branches {
		isCurrent := branchName == r.currentBranch
		annotation := r.Annotations[branchName]

		line := "  "
		if isCurrent {
			line += CurrentBranchSymbol + " "
		} else {
			line += BranchSymbol + " "
		}

		line += style.ColorBranchNameWithTrunk(branchName, isCurrent, r.isTrunk(branchName))
		line += r.FormatAnnotationColored(annotation)

		result = append(result, line)
	}

	return result
}
