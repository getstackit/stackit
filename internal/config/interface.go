package config

// Configurer is the interface for stackit configuration.
// The git-based GitConfig implements this interface.
type Configurer interface {
	// Initialization
	IsInitialized() bool

	// Trunk configuration
	Trunk() string
	AllTrunks() []string
	IsTrunk(branch string) bool

	// Branch naming
	BranchNamePattern() string
	GetBranchPattern() BranchPattern

	// Submit settings
	SubmitFooter() bool

	// Undo settings
	UndoStackDepth() int

	// Worktree settings
	WorktreeBasePath() string
	WorktreeAutoClean() bool

	// Merge settings
	MergeMethod() string

	// CI settings
	CICommand() string
	CITimeout() int

	// Split settings
	SplitHunkSelector() string

	// Concurrency settings
	MaxConcurrency() int

	// Navigation settings
	NavigationWhen() string
	NavigationMarker() string
	NavigationLocation() string
	NavigationShowMerged() bool

	// Project config access (.stackit.yaml). Returns nil if unloaded.
	ProjectConfig() *ProjectConfig

	// Hook approvals
	ApprovedHooks(phase string) []string
	IsHookApproved(phase, command string) bool
	ApprovedPostWorktreeCreateHooks() []string
	IsPostWorktreeCreateHookApproved(hook string) bool

	// Deprecated methods (for backwards compatibility)
	CombineCICommand() string
	CombineCITimeout() int
}

// Ensure GitConfig implements the interface
var _ Configurer = (*GitConfig)(nil)
