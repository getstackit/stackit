package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func registerMergeStories() {
	RegisterStory(Story{
		Name:        "Merge Type Selector",
		Category:    "Merge",
		Description: "The initial selector to choose what to merge (this branch, scope, or stack)",
		CreateModel: func() tea.Model {
			return NewSelectModel("What would you like to merge?", []SelectOption{
				{Label: "🌿 This branch — Merge the current branch and its stack", Value: "this"},
				{Label: "🏷️ Select a scope — Merge all branches in a specific scope", Value: "scope"},
				{Label: "📚 Select an entire stack — Merge a stack from its top branch", Value: "stack"},
			}, 0)
		},
	})

	RegisterStory(Story{
		Name:        "Merge Strategy Selector",
		Category:    "Merge",
		Description: "Selecting the merge strategy (bottom-up, top-down, or consolidate)",
		CreateModel: func() tea.Model {
			return NewSelectModel("Select merge strategy:", []SelectOption{
				{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom (recommended)", Value: "bottom-up"},
				{Label: "📦 Top-down — Squash all changes into one PR, merge once", Value: "top-down"},
				{Label: "🔀 Consolidate — Create single PR with all stack commits for atomic merge", Value: "consolidate"},
			}, 0)
		},
	})

	RegisterStory(Story{
		Name:        "Scope Selector",
		Category:    "Merge",
		Description: "Selecting a scope from a list of available scopes",
		CreateModel: func() tea.Model {
			return NewSelectModel("Select scope to merge:", []SelectOption{
				{Label: "API", Value: "API"},
				{Label: "UI", Value: "UI"},
				{Label: "Auth", Value: "Auth"},
				{Label: "Refactor", Value: "Refactor"},
			}, 0)
		},
	})

	RegisterStory(Story{
		Name:        "Stack Selector (Branch Selection)",
		Category:    "Merge",
		Description: "Selecting a leaf branch to merge an entire stack",
		CreateModel: func() tea.Model {
			return NewBranchSelectModel("Select a stack to merge (choose the top branch):", []BranchChoice{
				{Display: "  feature-3 (UI)", Value: "feature-3"},
				{Display: "  api-v2 (API)", Value: "api-v2"},
				{Display: "  auth-fix (Auth)", Value: "auth-fix"},
			}, 0)
		},
	})
}
