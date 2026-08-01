package config

import "github.com/getstackit/stackit/internal/github"

// Option describes a single configuration option for documentation and template generation.
type Option struct {
	// YAMLPath is the path in the YAML config file (e.g., "submit.footer")
	YAMLPath string
	// GitKey is the full git config key (e.g., "stackit.submit.footer")
	GitKey string
	// Description is a human-readable description of the option
	Description string
	// Default is the default value (nil if no default)
	Default any
	// ValidValues lists allowed values for enum-type options (nil if any value allowed)
	ValidValues []string
	// Example is an example value for the template (optional, used when Default is nil)
	Example string
	// IsArray indicates this is a multi-value option
	IsArray bool
	// Section groups related options together in the generated template
	Section string
	// Comment provides additional context in the template (e.g., "Placeholders: {username}, {date}")
	Comment string
}

// Section defines a group of related options for documentation and templates.
type Section struct {
	// Name is the internal identifier (matches Option.Section)
	Name string
	// Title is the display title for templates (empty = no header comment)
	Title string
	// DocsTitle is the display title for documentation (empty = skip in docs)
	DocsTitle string
}

// Sections defines the ordering and titles for config sections.
// This is the single source of truth for section organization.
var Sections = []Section{
	{Name: SectionTrunk, Title: "", DocsTitle: "Trunk branches"},
	{Name: SectionBranch, Title: "Branch naming pattern", DocsTitle: "Branch naming"},
	{Name: SectionStack, Title: "Stack topology", DocsTitle: "Stack topology"},
	{Name: SectionSubmit, Title: "PR submission settings", DocsTitle: "PR submission"},
	{Name: SectionMerge, Title: "Merge method: squash, merge, rebase", DocsTitle: "Merge settings"},
	{Name: SectionCI, Title: "CI validation", DocsTitle: "CI validation"},
	{Name: SectionUndo, Title: "Undo history", DocsTitle: "Other settings"},
	{Name: SectionWorktree, Title: "Worktree settings", DocsTitle: "Worktree settings"},
	{Name: SectionSplit, Title: "Split command", DocsTitle: "Split command"},
	{Name: SectionConcurrency, Title: "Concurrency", DocsTitle: ""}, // grouped with "Other settings"
	{Name: SectionNavigation, Title: "PR navigation display", DocsTitle: "PR navigation"},
	{Name: SectionHooks, Title: "Post-worktree-create hooks (require approval on first run)", DocsTitle: ""},
}

// Options is the registry of all configuration options.
// This is the single source of truth for all config keys and their metadata.
var Options = []Option{
	// Trunk section
	{
		YAMLPath:    YAMLPathTrunk,
		GitKey:      KeyTrunk,
		Description: "Primary trunk branch",
		Default:     DefaultTrunk,
		Section:     SectionTrunk,
	},
	{
		YAMLPath:    YAMLPathTrunks,
		GitKey:      KeyTrunks,
		Description: "Additional trunk branches (e.g., release branches)",
		IsArray:     true,
		Example:     "develop, release",
		Section:     SectionTrunk,
	},

	// Branch section
	{
		YAMLPath:    YAMLPathBranchPattern,
		GitKey:      KeyBranchPattern,
		Description: "Branch naming pattern",
		Comment:     "Placeholders: {username}, {date}, {message}, {scope}",
		Example:     "{username}/{date}/{message}",
		Section:     SectionBranch,
	},
	{
		YAMLPath:    YAMLPathStackShape,
		GitKey:      KeyStackShape,
		Description: "tree or linear (linear prevents forks below non-trunk branches)",
		Default:     StackShapeTree,
		ValidValues: ValidStackShapes,
		Section:     SectionStack,
	},

	// Submit section
	{
		YAMLPath:    YAMLPathSubmitFooter,
		GitKey:      KeySubmitFooter,
		Description: "Include navigation footer",
		Default:     DefaultSubmitFooter,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitDraft,
		GitKey:      KeySubmitDraft,
		Description: "Create as draft",
		Default:     DefaultSubmitDraft,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitGitHubStack,
		GitKey:      KeySubmitGitHubStack,
		Description: "Sync native GitHub Stack metadata for eligible linear PR chains",
		Default:     DefaultSubmitGitHubStack,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitWeb,
		GitKey:      KeySubmitWeb,
		Description: "Open in browser: always, created, never",
		Default:     DefaultSubmitWeb,
		ValidValues: ValidSubmitWeb,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitLabels,
		GitKey:      KeySubmitLabels,
		Description: "Default labels",
		IsArray:     true,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitReviewers,
		GitKey:      KeySubmitReviewers,
		Description: "Default reviewers",
		IsArray:     true,
		Section:     SectionSubmit,
	},
	{
		YAMLPath:    YAMLPathSubmitAssignees,
		GitKey:      KeySubmitAssignees,
		Description: "Default assignees",
		IsArray:     true,
		Section:     SectionSubmit,
	},

	// Merge section
	{
		YAMLPath:    YAMLPathMergeMethod,
		GitKey:      KeyMergeMethod,
		Description: "Merge method: squash, merge, rebase",
		ValidValues: ValidMergeMethods,
		Example:     string(github.MergeMethodSquash),
		Section:     SectionMerge,
	},

	// CI section
	{
		YAMLPath:    YAMLPathCICommand,
		GitKey:      KeyCICommand,
		Description: "Command to run",
		Example:     ciCommandExample,
		Section:     SectionCI,
	},
	{
		YAMLPath:    YAMLPathCITimeout,
		GitKey:      KeyCITimeout,
		Description: "Timeout in seconds",
		Default:     DefaultCITimeout,
		Section:     SectionCI,
	},

	// Undo section
	{
		YAMLPath:    YAMLPathUndoDepth,
		GitKey:      KeyUndoDepth,
		Description: "Max snapshots",
		Default:     DefaultUndoDepth,
		Section:     SectionUndo,
	},
	{
		YAMLPath:    YAMLPathUndoEnabled,
		GitKey:      KeyUndoEnabled,
		Description: "Take snapshots before mutations (set false to skip undo overhead)",
		Default:     DefaultUndoEnabled,
		Section:     SectionUndo,
	},

	// Worktree section
	{
		YAMLPath:    YAMLPathWorktreeBasePath,
		GitKey:      KeyWorktreeBasePath,
		Description: "Base directory (empty = auto)",
		Example:     "",
		Section:     SectionWorktree,
	},
	{
		YAMLPath:    YAMLPathWorktreeAutoClean,
		GitKey:      KeyWorktreeAutoClean,
		Description: "Clean during sync",
		Default:     DefaultWorktreeAutoClean,
		Section:     SectionWorktree,
	},

	// Split section
	{
		YAMLPath:    YAMLPathSplitHunkSelector,
		GitKey:      KeySplitHunkSelector,
		Description: "tui or git",
		Default:     DefaultSplitHunkSelector,
		ValidValues: ValidHunkSelectors,
		Section:     SectionSplit,
	},

	// Concurrency (top-level)
	{
		YAMLPath:    YAMLPathMaxConcurrency,
		GitKey:      KeyMaxConcurrency,
		Description: "0 = auto-detect",
		Default:     DefaultMaxConcurrency,
		Section:     SectionConcurrency,
	},

	// Navigation section
	{
		YAMLPath:    YAMLPathNavigationWhen,
		GitKey:      KeyNavigationWhen,
		Description: "always, never, multiple",
		Default:     DefaultNavigationWhen,
		ValidValues: ValidNavigationWhen,
		Section:     SectionNavigation,
	},
	{
		YAMLPath:    YAMLPathNavigationLocation,
		GitKey:      KeyNavigationLocation,
		Description: "body, comment, none",
		Default:     DefaultNavigationLocation,
		ValidValues: ValidNavigationLocation,
		Section:     SectionNavigation,
	},
	{
		YAMLPath:    YAMLPathNavigationMarker,
		GitKey:      KeyNavigationMarker,
		Description: "Current branch marker",
		Default:     DefaultNavigationMarker,
		Section:     SectionNavigation,
	},
	{
		YAMLPath:    YAMLPathNavigationShowMerged,
		GitKey:      KeyNavigationShowMerged,
		Description: "Show merged history",
		Default:     DefaultNavigationShowMerged,
		Section:     SectionNavigation,
	},

	// Hooks section (special - not directly settable via config set)
	// Note: YAMLPath is the team config format (defines hooks to run),
	// while GitKey is for personal approval tracking (which hooks user has approved).
	// This is intentional: teams define hooks in .stackit.yaml, users approve them locally.
	{
		YAMLPath:    YAMLPathHooksPostWorktree,
		GitKey:      KeyApprovedHooks,
		Description: "Commands to run after creating a worktree",
		IsArray:     true,
		Example:     "npm install, mise install",
		Section:     SectionHooks,
	},
}

// GetOptionByGitKey returns the Option for a given git key, or nil if not found.
func GetOptionByGitKey(gitKey string) *Option {
	for i := range Options {
		if Options[i].GitKey == gitKey {
			return &Options[i]
		}
	}
	return nil
}

// GetOptionByYAMLPath returns the Option for a given YAML path, or nil if not found.
func GetOptionByYAMLPath(yamlPath string) *Option {
	for i := range Options {
		if Options[i].YAMLPath == yamlPath {
			return &Options[i]
		}
	}
	return nil
}

// AllGitKeys returns all git config keys from the registry.
func AllGitKeys() []string {
	keys := make([]string, len(Options))
	for i, opt := range Options {
		keys[i] = opt.GitKey
	}
	return keys
}

// GetOptionsForSection returns all options belonging to the given section.
func GetOptionsForSection(section string) []Option {
	var opts []Option
	for _, opt := range Options {
		if opt.Section == section {
			opts = append(opts, opt)
		}
	}
	return opts
}

// GetSectionByName returns the Section with the given name, or nil if not found.
func GetSectionByName(name string) *Section {
	for i := range Sections {
		if Sections[i].Name == name {
			return &Sections[i]
		}
	}
	return nil
}
