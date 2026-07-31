package config

import "github.com/getstackit/stackit/internal/github"

// Git config keys for stackit configuration.
// All keys are prefixed with "stackit." to namespace them within git config.
const (
	// KeyTrunk is the primary trunk branch name.
	KeyTrunk = "stackit.trunk"
	// KeyTrunks stores additional trunk branches (multi-value).
	KeyTrunks = "stackit.trunks"
	// KeyBranchPattern is the pattern used for generating branch names.
	KeyBranchPattern = "stackit.branch.pattern"
	// KeyStackShape controls whether Stackit allows tree-shaped or only linear stacks.
	KeyStackShape = "stackit.stack.shape"
	// KeySubmitFooter controls whether to include PR footer in submissions.
	KeySubmitFooter = "stackit.submit.footer"
	// KeyUndoDepth is the maximum number of undo snapshots to keep.
	KeyUndoDepth = "stackit.undo.depth"
	// KeyUndoEnabled controls whether undo snapshots are taken at all.
	// Set to false to skip snapshot overhead for users who never run `stackit undo`.
	KeyUndoEnabled = "stackit.undo.enabled"
	// KeyWorktreeBasePath is the base path for worktrees.
	KeyWorktreeBasePath = "stackit.worktree.basePath"
	// KeyWorktreeAutoClean controls automatic worktree cleanup during sync.
	KeyWorktreeAutoClean = "stackit.worktree.autoClean"
	// KeyMergeMethod is the preferred merge method (squash, merge, rebase).
	KeyMergeMethod = "stackit.merge.method"
	// KeyCICommand is the unified CI validation command.
	KeyCICommand = "stackit.ci.command"
	// KeyCITimeout is the CI command timeout in seconds.
	KeyCITimeout = "stackit.ci.timeout"
	// KeySplitHunkSelector is the hunk selector mode for split (tui or git).
	KeySplitHunkSelector = "stackit.split.hunkSelector"
	// KeyApprovedHooks stores approved post-worktree-create hooks (multi-value).
	//
	// Deprecated: kept as a compat read-path. New approvals are written under
	// the per-phase family KeyApprovedHookPrefix+<phase>.
	KeyApprovedHooks = "stackit.hooks.approvedPostWorktreeCreate"
	// KeyApprovedHookPrefix is the prefix for per-phase hook approval keys.
	// Full key form: <prefix><phase>, e.g. "stackit.hooks.approved.pre-modify".
	KeyApprovedHookPrefix = "stackit.hooks.approved."
	// KeyMaxConcurrency is the maximum number of concurrent validation operations.
	KeyMaxConcurrency = "stackit.maxConcurrency"
	// KeyNavigationWhen controls when navigation is displayed (always/never/multiple).
	KeyNavigationWhen = "stackit.navigation.when"
	// KeyNavigationMarker is the custom marker symbol for the current branch.
	KeyNavigationMarker = "stackit.navigation.marker"
	// KeyNavigationLocation controls where navigation appears (body/comment).
	KeyNavigationLocation = "stackit.navigation.location"
	// KeyNavigationShowMerged controls whether to show merged branch history.
	KeyNavigationShowMerged = "stackit.navigation.showMerged"
	// KeySubmitDraft controls whether to create PRs as drafts by default.
	KeySubmitDraft = "stackit.submit.draft"
	// KeySubmitWeb controls when to open PRs in browser (always/created/never).
	KeySubmitWeb = "stackit.submit.web"
	// KeySubmitLabels stores default labels for PRs (multi-value).
	KeySubmitLabels = "stackit.submit.labels"
	// KeySubmitReviewers stores default reviewers for PRs (multi-value).
	KeySubmitReviewers = "stackit.submit.reviewers"
	// KeySubmitAssignees stores default assignees for PRs (multi-value).
	KeySubmitAssignees = "stackit.submit.assignees"
)

// YAML path constants for configuration options.
// These are the paths used in .stackit.yaml files.
const (
	YAMLPathTrunk                = "trunk"
	YAMLPathTrunks               = "trunks"
	YAMLPathBranchPattern        = "branch.pattern"
	YAMLPathStackShape           = "stack.shape"
	YAMLPathSubmitFooter         = "submit.footer"
	YAMLPathSubmitDraft          = "submit.draft"
	YAMLPathSubmitWeb            = "submit.web"
	YAMLPathSubmitLabels         = "submit.labels"
	YAMLPathSubmitReviewers      = "submit.reviewers"
	YAMLPathSubmitAssignees      = "submit.assignees"
	YAMLPathMergeMethod          = "merge.method"
	YAMLPathCICommand            = "ci.command"
	YAMLPathCITimeout            = "ci.timeout"
	YAMLPathUndoDepth            = "undo.depth"
	YAMLPathUndoEnabled          = "undo.enabled"
	YAMLPathWorktreeBasePath     = "worktree.basePath"
	YAMLPathWorktreeAutoClean    = "worktree.autoClean"
	YAMLPathSplitHunkSelector    = "split.hunkSelector"
	YAMLPathMaxConcurrency       = "maxConcurrency"
	YAMLPathNavigationWhen       = "navigation.when"
	YAMLPathNavigationLocation   = "navigation.location"
	YAMLPathNavigationMarker     = "navigation.marker"
	YAMLPathNavigationShowMerged = "navigation.showMerged"
	YAMLPathHooksPostWorktree    = "hooks.post-worktree-create"
)

// ciCommandExample is the example CI command used in templates and documentation.
const ciCommandExample = "make test"

// Default values for configuration.
const (
	// DefaultTrunk is the default trunk branch name.
	DefaultTrunk = "main"
	// DefaultSubmitFooter is whether to include PR footer by default.
	DefaultSubmitFooter = true
	// DefaultUndoDepth is the default number of undo snapshots to keep.
	DefaultUndoDepth = 10
	// DefaultUndoEnabled is whether undo snapshots are taken by default.
	DefaultUndoEnabled = true
	// DefaultWorktreeAutoClean is whether to auto-clean worktrees by default.
	DefaultWorktreeAutoClean = true
	// DefaultCITimeout is the default CI timeout in seconds (10 minutes).
	DefaultCITimeout = 600
	// DefaultSplitHunkSelector is the default hunk selector mode.
	DefaultSplitHunkSelector = "tui"
	// DefaultMaxConcurrency is the default max concurrent operations (0 = auto).
	DefaultMaxConcurrency = 0
	// DefaultNavigationWhen is the default navigation display mode.
	DefaultNavigationWhen = "multiple"
	// DefaultNavigationMarker is the default marker for the current branch.
	DefaultNavigationMarker = "👈"
	// DefaultNavigationLocation is the default location for navigation (PR body).
	DefaultNavigationLocation = "body"
	// DefaultNavigationShowMerged is whether to show merged history by default.
	DefaultNavigationShowMerged = true
	// DefaultSubmitDraft is whether to create PRs as drafts by default.
	DefaultSubmitDraft = false
	// DefaultSubmitWeb is when to open PRs in browser by default.
	DefaultSubmitWeb = "never"
)

// Config section name constants. These identify configuration sections used in
// the metadata registry, YAML validation, and documentation generation.
const (
	SectionTrunk       = "trunk"
	SectionTrunks      = "trunks" // array variant of trunk for YAML and validation
	SectionBranch      = "branch"
	SectionStack       = "stack"
	SectionSubmit      = "submit"
	SectionMerge       = "merge"
	SectionCI          = "ci"
	SectionUndo        = "undo"
	SectionWorktree    = "worktree"
	SectionSplit       = "split"
	SectionConcurrency = "concurrency"
	SectionNavigation  = "navigation"
	SectionHooks       = "hooks"
)

// Stack shape constants. Linear stacks are compatible with GitHub's native
// stacked pull request model; tree remains the default Stackit topology.
const (
	StackShapeTree   = "tree"
	StackShapeLinear = "linear"
)

// ValidStackShapes contains the allowed values for stack.shape.
var ValidStackShapes = []string{StackShapeTree, StackShapeLinear}

// Navigation when constants (valid values for KeyNavigationWhen).
const (
	NavigationWhenAlways   = "always"
	NavigationWhenNever    = "never"
	NavigationWhenMultiple = "multiple"
)

// ValidMergeMethods contains the allowed merge method values (the string
// names of github.ValidMergeMethods, for config metadata and error messages).
var ValidMergeMethods = []string{
	string(github.MergeMethodSquash),
	string(github.MergeMethodMerge),
	string(github.MergeMethodRebase),
}

// ValidHunkSelectors contains the allowed hunk selector values.
var ValidHunkSelectors = []string{"tui", "git"}

// ValidNavigationWhen contains the allowed navigation when values.
var ValidNavigationWhen = []string{NavigationWhenAlways, NavigationWhenNever, NavigationWhenMultiple}

// Navigation location constants.
const (
	NavigationLocationBody    = "body"
	NavigationLocationComment = "comment"
	NavigationLocationNone    = "none"
)

// ValidNavigationLocation contains the allowed navigation location values.
// "none" is an alias for disabling navigation (equivalent to when=never).
var ValidNavigationLocation = []string{NavigationLocationBody, NavigationLocationComment, NavigationLocationNone}

// Submit web constants.
const (
	SubmitWebAlways  = "always"
	SubmitWebCreated = "created"
	SubmitWebNever   = "never"
)

// ValidSubmitWeb contains the allowed submit.web values.
var ValidSubmitWeb = []string{SubmitWebAlways, SubmitWebCreated, SubmitWebNever}

// Hook phase identifiers. These are the suffix used after KeyApprovedHookPrefix
// and as the YAML key under .stackit.yaml's `hooks:` block.
const (
	PhasePostWorktreeCreate = "post-worktree-create"
)
