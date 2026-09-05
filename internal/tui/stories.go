package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/getstackit/stackit/internal/tui/components/submit"
	"github.com/getstackit/stackit/internal/tui/components/tree"
)

// Story represents a specific state of a TUI component
type Story struct {
	Name        string
	Category    string
	Description string
	CreateModel func() tea.Model
}

// Stories is a registry of all component stories
var Stories = []Story{}

// RegisterStory registers a new component story
func RegisterStory(story Story) {
	Stories = append(Stories, story)
}

func init() {
	registerTreeStories()
	registerLargeTreeStories()
	registerSubmitStories()
	registerMergeStories()
	registerPromptStories()
}

func registerTreeStories() {
	RegisterStory(Story{
		Name:        "Linear Stack",
		Category:    "Tree",
		Description: "A simple 3-branch linear stack with PR states",
		CreateModel: func() tea.Model {
			mock := &tree.MockTreeData{
				CurrentVal: "feature-2",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":      {"feature-1"},
					"feature-1": {"feature-2"},
					"feature-2": {},
				},
				ParentsMap: map[string]string{
					"feature-1": "main",
					"feature-2": "feature-1",
				},
				FixedMap: map[string]bool{
					"main":      true,
					"feature-1": true,
					"feature-2": true,
				},
			}
			renderer := tree.NewRenderer(mock)

			pr1 := 101
			renderer.SetAnnotation("feature-1", tree.BranchAnnotation{
				PRNumber:      &pr1,
				Scope:         "API",
				ExplicitScope: "API",
				CommitCount:   2,
				LinesAdded:    50,
				LinesDeleted:  10,
				CheckStatus:   tree.CheckStatusPassing,
				ReviewStatus:  "Approved",
			})

			pr2 := 102
			renderer.SetAnnotation("feature-2", tree.BranchAnnotation{
				PRNumber:      &pr2,
				Scope:         "UI",
				ExplicitScope: "UI",
				CommitCount:   5,
				LinesAdded:    120,
				LinesDeleted:  5,
				CheckStatus:   tree.CheckStatusPending,
			})

			return tree.NewModel(renderer)
		},
	})

	RegisterStory(Story{
		Name:        "PR States",
		Category:    "Tree",
		Description: "Shows different PR states: draft, merged, failing CI, changes requested",
		CreateModel: func() tea.Model {
			mock := &tree.MockTreeData{
				CurrentVal: "feature-active",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":            {"feature-merged", "feature-draft", "feature-active"},
					"feature-merged":  {},
					"feature-draft":   {},
					"feature-active":  {"feature-failing"},
					"feature-failing": {},
				},
				ParentsMap: map[string]string{
					"feature-merged":  "main",
					"feature-draft":   "main",
					"feature-active":  "main",
					"feature-failing": "feature-active",
				},
				FixedMap: map[string]bool{
					"main":            true,
					"feature-merged":  true,
					"feature-draft":   true,
					"feature-active":  true,
					"feature-failing": false, // restack suggested
				},
			}
			renderer := tree.NewRenderer(mock)

			// Merged PR - should be dimmed and collapsed
			pr1 := 90
			renderer.SetAnnotation("feature-merged", tree.BranchAnnotation{
				PRNumber:      &pr1,
				Scope:         "CORE",
				ExplicitScope: "CORE",
				PRState:       "MERGED",
				CommitCount:   3,
				LinesAdded:    100,
			})

			// Draft PR
			pr2 := 95
			renderer.SetAnnotation("feature-draft", tree.BranchAnnotation{
				PRNumber:    &pr2,
				Scope:       "CORE",
				IsDraft:     true,
				CommitCount: 1,
				LinesAdded:  20,
			})

			// Active PR with approval
			pr3 := 100
			renderer.SetAnnotation("feature-active", tree.BranchAnnotation{
				PRNumber:      &pr3,
				Scope:         "API",
				ExplicitScope: "API",
				CommitCount:   2,
				LinesAdded:    80,
				LinesDeleted:  15,
				CheckStatus:   tree.CheckStatusPassing,
				ReviewStatus:  "Approved",
			})

			// Failing CI with changes requested
			pr4 := 105
			renderer.SetAnnotation("feature-failing", tree.BranchAnnotation{
				PRNumber:     &pr4,
				Scope:        "API",
				CommitCount:  4,
				LinesAdded:   200,
				LinesDeleted: 50,
				CheckStatus:  tree.CheckStatusFailing,
				ReviewStatus: "Changes Requested",
			})

			return tree.NewModel(renderer)
		},
	})

	RegisterStory(Story{
		Name:        "Complex Branching",
		Category:    "Tree",
		Description: "A tree with multiple branches at the same level and different scopes",
		CreateModel: func() tea.Model {
			mock := &tree.MockTreeData{
				CurrentVal: "auth-fix",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":      {"base-api", "base-auth"},
					"base-api":  {"api-v2", "api-docs"},
					"base-auth": {"auth-fix"},
					"api-v2":    {},
					"api-docs":  {},
					"auth-fix":  {},
				},
				ParentsMap: map[string]string{
					"base-api":  "main",
					"base-auth": "main",
					"api-v2":    "base-api",
					"api-docs":  "base-api",
					"auth-fix":  "base-auth",
				},
				FixedMap: map[string]bool{
					"main":      true,
					"base-api":  true,
					"base-auth": true,
					"api-v2":    true,
					"api-docs":  false, // restack suggested
					"auth-fix":  true,
				},
			}
			renderer := tree.NewRenderer(mock)

			// Base branch without PR - shows just stats
			renderer.SetAnnotation("base-api", tree.BranchAnnotation{
				Scope:         "API",
				ExplicitScope: "API",
				CommitCount:   1,
				LinesAdded:    15,
			})

			pr3 := 103
			renderer.SetAnnotation("api-v2", tree.BranchAnnotation{
				PRNumber:     &pr3,
				Scope:        "API",
				CommitCount:  10,
				LinesAdded:   400,
				LinesDeleted: 25,
				ReviewStatus: "Changes Requested",
				CheckStatus:  tree.CheckStatusFailing,
			})

			// Branch without PR, restack suggested
			renderer.SetAnnotation("api-docs", tree.BranchAnnotation{
				Scope:       "API",
				CommitCount: 1,
				LinesAdded:  20,
			})

			renderer.SetAnnotation("base-auth", tree.BranchAnnotation{
				Scope:         "AUTH",
				ExplicitScope: "AUTH",
				CommitCount:   1,
				LinesAdded:    10,
			})

			pr4 := 104
			renderer.SetAnnotation("auth-fix", tree.BranchAnnotation{
				PRNumber:     &pr4,
				Scope:        "AUTH",
				CommitCount:  3,
				LinesAdded:   30,
				LinesDeleted: 30,
				CheckStatus:  tree.CheckStatusPassing,
				ReviewStatus: "Approved",
			})

			return tree.NewModel(renderer)
		},
	})

	RegisterStory(Story{
		Name:        "No PRs",
		Category:    "Tree",
		Description: "A stack where no PRs have been submitted yet - shows stats only",
		CreateModel: func() tea.Model {
			mock := &tree.MockTreeData{
				CurrentVal: "feature-c",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":      {"feature-a"},
					"feature-a": {"feature-b"},
					"feature-b": {"feature-c"},
					"feature-c": {},
				},
				ParentsMap: map[string]string{
					"feature-a": "main",
					"feature-b": "feature-a",
					"feature-c": "feature-b",
				},
				FixedMap: map[string]bool{
					"main":      true,
					"feature-a": true,
					"feature-b": true,
					"feature-c": true,
				},
			}
			renderer := tree.NewRenderer(mock)

			// Single commit - should NOT show commit count
			renderer.SetAnnotation("feature-a", tree.BranchAnnotation{
				Scope:         "CORE",
				ExplicitScope: "CORE",
				CommitCount:   1,
				LinesAdded:    50,
				LinesDeleted:  10,
			})

			// Multiple commits - SHOULD show commit count
			renderer.SetAnnotation("feature-b", tree.BranchAnnotation{
				Scope:        "CORE",
				CommitCount:  3,
				LinesAdded:   120,
				LinesDeleted: 0,
			})

			// Zero lines changed - should NOT show +0/-0
			renderer.SetAnnotation("feature-c", tree.BranchAnnotation{
				Scope:        "CORE",
				CommitCount:  1,
				LinesAdded:   0,
				LinesDeleted: 0,
			})

			return tree.NewModel(renderer)
		},
	})

	RegisterStory(Story{
		Name:        "Stack Submission",
		Category:    "Tree",
		Description: "The configuration view before submitting a stack, showing planned actions",
		CreateModel: func() tea.Model {
			mock := &tree.MockTreeData{
				CurrentVal: "feature-3",
				TrunkVal:   "main",
				ChildrenMap: map[string][]string{
					"main":      {"feature-1"},
					"feature-1": {"feature-2"},
					"feature-2": {"feature-3"},
					"feature-3": {},
				},
				ParentsMap: map[string]string{
					"feature-1": "main",
					"feature-2": "feature-1",
					"feature-3": "feature-2",
				},
				FixedMap: map[string]bool{
					"main":      true,
					"feature-1": true,
					"feature-2": true,
					"feature-3": true,
				},
			}
			renderer := tree.NewRenderer(mock)

			pr1 := 101
			renderer.SetAnnotation("feature-1", tree.BranchAnnotation{
				PRNumber:      &pr1,
				PRAction:      "skip",
				CustomLabel:   "(up to date)",
				Scope:         "CORE",
				ExplicitScope: "CORE",
				CommitCount:   0,
			})

			pr2 := 102
			renderer.SetAnnotation("feature-2", tree.BranchAnnotation{
				PRNumber:      &pr2,
				PRAction:      "update",
				Scope:         "API",
				ExplicitScope: "API",
				CommitCount:   3,
				LinesAdded:    45,
				LinesDeleted:  12,
			})

			renderer.SetAnnotation("feature-3", tree.BranchAnnotation{
				PRAction:      "create",
				Scope:         "UI",
				ExplicitScope: "UI",
				CommitCount:   5,
				LinesAdded:    120,
				LinesDeleted:  5,
			})

			model := tree.NewModel(renderer)
			model.Options.HideStats = true
			return model
		},
	})
}

// newSimulationSubmitModel builds the submit model as it looks when the TUI
// starts: the submission set is known and every branch is pending.
func newSimulationSubmitModel() *submit.Model {
	return submit.NewModel([]submit.Item{
		{BranchName: "feature-2", Action: submit.ActionUpdate, Status: submit.StatusPending},
		{BranchName: "feature-3", Action: submit.ActionCreate, Status: submit.StatusPending},
	})
}

func registerSubmitStories() {
	RegisterStory(Story{
		Name:        "Full Submission",
		Category:    "Submit",
		Description: "A simulated full submission process with state transitions",
		CreateModel: func() tea.Model {
			return &submitSimulationModel{
				submitModel: newSimulationSubmitModel(),
				startTime:   time.Now(),
			}
		},
	})

	RegisterStory(Story{
		Name:        "Submission Error",
		Category:    "Submit",
		Description: "A submission with an error on one of the branches",
		CreateModel: func() tea.Model {
			m := submit.NewModel([]submit.Item{
				{BranchName: "feature-1", Action: "update", Status: submit.StatusDone, URL: "https://github.com/owner/repo/pull/101"},
				{BranchName: "feature-2", Action: "update", Status: submit.StatusError, Error: fmt.Errorf("failed to push branch: remote rejected")},
				{BranchName: "feature-3", Action: "create", Status: submit.StatusPending},
			})
			return m
		},
	})
}

type submitSimulationModel struct {
	submitModel *submit.Model
	step        int
	startTime   time.Time
}

func (m *submitSimulationModel) Init() tea.Cmd {
	return tea.Batch(
		m.submitModel.Init(),
		m.nextTick(),
	)
}

func (m *submitSimulationModel) nextTick() tea.Cmd {
	delay := 1 * time.Second
	if m.step == 0 {
		delay = 100 * time.Millisecond
	}
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return simulationTickMsg(m.step)
	})
}

type simulationTickMsg int

func (m *submitSimulationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case simulationTickMsg:
		if int(msg) == -1 {
			// Reset simulation
			m.step = 0
			m.submitModel = newSimulationSubmitModel()
			return m, m.nextTick()
		}

		m.step++
		var cmds []tea.Cmd

		switch m.step {
		case 1:
			_, c := m.submitModel.Update(submit.ProgressUpdateMsg{BranchName: "feature-2", Status: submit.StatusSubmitting})
			cmds = append(cmds, c, m.nextTick())
		case 2:
			m.submitModel.Update(submit.ProgressUpdateMsg{BranchName: "feature-2", Status: submit.StatusDone, URL: "https://github.com/owner/repo/pull/102"})
			_, c := m.submitModel.Update(submit.ProgressUpdateMsg{BranchName: "feature-3", Status: submit.StatusSubmitting})
			cmds = append(cmds, c, m.nextTick())
		case 3:
			_, c := m.submitModel.Update(submit.ProgressUpdateMsg{BranchName: "feature-3", Status: submit.StatusDone, URL: "https://github.com/owner/repo/pull/103"})
			cmds = append(cmds, c, m.nextTick())
			// The final state stays visible until the reset tick.
			// We don't send ProgressCompleteMsg because it would trigger tea.Quit,
			// and we don't set Done because the view blanks once Done is set.
			cmds = append(cmds, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
				return simulationTickMsg(-1) // Signal reset
			}))
		}

		return m, tea.Batch(cmds...)

	case tea.KeyPressMsg:
		if msg.String() == KeyQuit || msg.String() == KeyEsc || msg.String() == KeyCtrlC {
			return m, tea.Quit
		}
	}

	newModel, cmd := m.submitModel.Update(msg)
	m.submitModel = newModel.(*submit.Model)
	return m, cmd
}

func (m *submitSimulationModel) View() tea.View {
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
	return tea.NewView(fmt.Sprint(m.submitModel.View().Content) + "\n" +
		helpStyle.Render(fmt.Sprintf("Simulation step: %d/3", m.step)) + "\n" +
		helpStyle.Render("q: back"))
}
