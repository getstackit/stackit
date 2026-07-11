package branch

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/split"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	splitcomp "github.com/getstackit/stackit/internal/tui/components/split"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// NewSplitUI creates a runner and handler pair for split operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewSplitUI(out output.Output, logger output.Logger) (*tui.Runner, split.Handler) {
	if utils.IsInteractive() {
		handler := NewTUISplitHandler(out, logger)
		return nil, handler
	}
	return nil, NewSimpleSplitHandler(out)
}

// SimpleSplitHandler provides streaming text output for split operations
type SimpleSplitHandler struct {
	common.BaseHandler
	branchName  string
	newBranches []string
}

// NewSimpleSplitHandler creates a new SimpleSplitHandler
func NewSimpleSplitHandler(out output.Output) *SimpleSplitHandler {
	return &SimpleSplitHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of split
func (h *SimpleSplitHandler) Start(branchName string, _ split.Style) {
	h.Lock()
	defer h.Unlock()
	h.branchName = branchName
	h.newBranches = nil
}

// OnStep is called for each step in the split process
func (h *SimpleSplitHandler) OnStep(_ split.Step, _ handler.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnBranchCreated is called when a new branch is created during split
func (h *SimpleSplitHandler) OnBranchCreated(branchName string) {
	h.Lock()
	defer h.Unlock()
	h.newBranches = append(h.newBranches, branchName)
	h.Output.Info("Created branch %s", style.ColorBranchName(branchName))
}

// Complete is called when split finishes
func (h *SimpleSplitHandler) Complete(result split.ActionResult) {
	h.Lock()
	defer h.Unlock()

	if len(result.NewBranches) > 0 {
		h.Output.Info("Split %s into %d branches",
			style.ColorBranchName(result.OriginalBranch),
			len(result.NewBranches))
	}
}

// PromptCommitMessageWithContext returns a generated default message in non-interactive mode
func (h *SimpleSplitHandler) PromptCommitMessageWithContext(ctx split.CommitMessageContext) (string, error) {
	return splitcomp.GenerateDefaultCommitMessage(ctx.Files, ctx.OriginalCommitMessage), nil
}

// TUISplitHandler provides interactive TUI prompts for split operations.
// It uses a unified model with state machine for type/direction selection,
// and delegates to full-screen TUI components for hunk selection.
type TUISplitHandler struct {
	*SimpleSplitHandler
	logger output.Logger
	runner *tui.Runner
	model  *splitcomp.Model
}

// NewTUISplitHandler creates a new TUISplitHandler
func NewTUISplitHandler(out output.Output, logger output.Logger) *TUISplitHandler {
	return &TUISplitHandler{
		SimpleSplitHandler: NewSimpleSplitHandler(out),
		logger:             logger,
	}
}

// Cleanup restores terminal state
func (h *TUISplitHandler) Cleanup() {
	if h.runner != nil {
		h.runner.Cleanup()
		h.runner = nil
	}
	h.SimpleSplitHandler.Cleanup()
}

// IsInteractive returns true as this handler supports interactive prompts
func (h *TUISplitHandler) IsInteractive() bool {
	return true
}

// PromptSplitType asks the user to choose between available split types.
// Uses the unified split model's type selection state.
func (h *TUISplitHandler) PromptSplitType(availableTypes []split.TypeChoice) (split.Style, error) {
	if !utils.IsInteractive() {
		return "", errors.ErrCanceled
	}

	// Convert types to component format
	compTypes := make([]splitcomp.TypeChoice, len(availableTypes))
	for i, t := range availableTypes {
		compTypes[i] = splitcomp.TypeChoice{
			Style:       splitcomp.Style(t.Style),
			Label:       t.Label,
			Description: t.Description,
			Available:   t.Available,
		}
	}

	// Create a model configured for type selection only
	cfg := splitcomp.Config{
		AvailableTypes: compTypes,
	}
	model := splitcomp.NewModel(cfg)

	// Run as standalone program (no alt screen to avoid flashing between views)
	p := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if m, ok := finalModel.(*splitcomp.Model); ok {
		result := m.GetResult()
		if result.Canceled {
			return "", errors.ErrCanceled
		}
		if result.Error != nil {
			return "", result.Error
		}
		return split.Style(result.Style), nil
	}

	return "", fmt.Errorf("unexpected model type")
}

// PromptDirection asks the user where to place the new branch using an interactive tree view.
// Uses the unified split model's direction selection state.
func (h *TUISplitHandler) PromptDirection(ctx split.DirectionContext) (split.Direction, error) {
	if !utils.IsInteractive() {
		return "", errors.ErrCanceled
	}

	// Use the existing direction selector which has the tree preview
	selected, err := tui.PromptDirectionSelect(ctx.Engine, ctx.CurrentBranch, ctx.ParentBranch, ctx.Children)
	if err != nil {
		return "", err
	}

	return split.Direction(selected), nil
}

// ShowHunkSummary displays a summary of the remaining changes
func (h *TUISplitHandler) ShowHunkSummary(diff string) {
	h.Output.Info("Remaining changes:")
	for line := range strings.SplitSeq(diff, "\n") {
		h.Output.Info("  %s", line)
	}
	h.Output.Info("")
}

// PromptCommitMessage asks the user to enter or edit a commit message.
// Uses Pause/Resume if a runner is active to release the terminal for the editor.
func (h *TUISplitHandler) PromptCommitMessage(defaultMsg string) (string, error) {
	if !utils.IsInteractive() {
		return defaultMsg, nil
	}

	// Pause TUI if running to release terminal for external editor
	if h.runner != nil {
		h.runner.Pause()
		defer h.runner.Resume()
	}

	msg, err := tui.OpenEditor(defaultMsg, "COMMIT_EDITMSG-*")
	if err != nil {
		return "", err
	}
	return utils.CleanCommitMessage(msg), nil
}

// PromptCommitMessageWithContext prompts for commit message with full context displayed.
// Uses an inline textarea editor that shows the split context (files, direction, etc.).
func (h *TUISplitHandler) PromptCommitMessageWithContext(ctx split.CommitMessageContext) (string, error) {
	if !utils.IsInteractive() {
		// In non-interactive mode, generate a sensible default
		return splitcomp.GenerateDefaultCommitMessage(ctx.Files, ctx.OriginalCommitMessage), nil
	}

	// Generate a sensible default commit message based on original commit
	defaultMsg := splitcomp.GenerateDefaultCommitMessage(ctx.Files, ctx.OriginalCommitMessage)

	// Create the inline commit editor model
	editorCfg := splitcomp.CommitEditorConfig{
		DefaultMessage: defaultMsg,
		Files:          ctx.Files,
		Direction:      splitcomp.Direction(ctx.Direction),
		CurrentBranch:  ctx.CurrentBranch,
	}
	model := splitcomp.NewCommitEditorModel(editorCfg)

	// Run as standalone program
	p := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if m, ok := finalModel.(*splitcomp.CommitEditorModel); ok {
		if m.Canceled() {
			return "", errors.ErrCanceled
		}
		msg := m.Message()
		if msg == "" {
			return "", fmt.Errorf("commit message cannot be empty")
		}
		return msg, nil
	}

	return "", fmt.Errorf("unexpected model type")
}

// PromptBranchName asks the user to enter a branch name
func (h *TUISplitHandler) PromptBranchName(defaultName string, sessionNames []string, allBranchNames *engine.BranchSet, originalBranchName string) (string, error) {
	if !utils.IsInteractive() {
		return defaultName, nil
	}

	// Pause TUI if running to release terminal for prompt
	if h.runner != nil {
		h.runner.Pause()
		defer h.runner.Resume()
	}

	branchName, err := tui.PromptTextInput(
		fmt.Sprintf("Enter branch name (default: %s):", defaultName),
		defaultName,
	)
	if err != nil {
		return "", err
	}

	if branchName == "" {
		branchName = defaultName
	}

	// Validate: empty names not allowed
	if strings.TrimSpace(branchName) == "" {
		return "", fmt.Errorf("branch name cannot be empty")
	}

	// Validate: check for invalid characters and Git branch name rules
	if err := utils.ValidateBranchName(branchName); err != nil {
		return "", err
	}

	// Validate: don't allow names already picked in this split session
	if slices.Contains(sessionNames, branchName) {
		return "", fmt.Errorf("branch name %q is already used by another branch in this split", branchName)
	}

	// Validate: don't allow existing branch names (except the original being split)
	if branchName != originalBranchName && allBranchNames.Contains(branchName) {
		return "", fmt.Errorf("branch name %q already exists in the repository", branchName)
	}

	return branchName, nil
}

// PromptContinueOrCancel asks user whether to continue after no changes were staged
func (h *TUISplitHandler) PromptContinueOrCancel() (bool, error) {
	if !utils.IsInteractive() {
		return false, nil
	}

	// Pause TUI if running to release terminal for prompt
	if h.runner != nil {
		h.runner.Pause()
		defer h.runner.Resume()
	}

	options := []tui.SelectOption{
		{Label: "Try again", Value: "continue"},
		{Label: "Cancel split", Value: "cancel"},
	}

	selected, err := tui.PromptSelect("No changes staged. What would you like to do?", options, 0)
	if err != nil {
		return false, err
	}

	return selected == "continue", nil
}

// PromptEditCommitMessage asks whether the user wants to edit the commit message
func (h *TUISplitHandler) PromptEditCommitMessage() (bool, error) {
	if !utils.IsInteractive() {
		return false, nil
	}

	// Pause TUI if running to release terminal for prompt
	if h.runner != nil {
		h.runner.Pause()
		defer h.runner.Resume()
	}

	return tui.PromptConfirm("Edit commit message?", true)
}

// PromptSelectHunks displays the hunk selector TUI and returns selected hunks.
// This uses a separate full-screen tea.Program for the hunk selection.
func (h *TUISplitHandler) PromptSelectHunks(hunks []git.Hunk) ([]git.Hunk, error) {
	if !utils.IsInteractive() {
		return nil, errors.ErrCanceled
	}

	// Pause any parent TUI to release terminal
	if h.runner != nil {
		h.runner.Pause()
		defer h.runner.Resume()
	}

	return tui.PromptSelectHunks(hunks)
}

// StartWizard initializes the unified split model for wizard mode.
// This is called by the action layer when starting a wizard-based split.
func (h *TUISplitHandler) StartWizard(eng engine.Engine, branch engine.Branch, preselectedStyle split.Style, preselectedDir split.Direction, availableTypes []split.TypeChoice) {
	// Convert types to component format
	compTypes := make([]splitcomp.TypeChoice, len(availableTypes))
	for i, t := range availableTypes {
		compTypes[i] = splitcomp.TypeChoice{
			Style:       splitcomp.Style(t.Style),
			Label:       t.Label,
			Description: t.Description,
			Available:   t.Available,
		}
	}

	cfg := splitcomp.Config{
		Engine:               eng,
		Branch:               branch,
		PreselectedStyle:     splitcomp.Style(preselectedStyle),
		PreselectedDirection: splitcomp.Direction(preselectedDir),
		AvailableTypes:       compTypes,
	}

	h.model = splitcomp.NewModel(cfg)

	// Build existing branch names for validation
	existingNames := make(map[string]bool)
	for _, b := range eng.AllBranches() {
		existingNames[b.GetName()] = true
	}
	h.model.SetExistingBranchNames(existingNames)
	h.model.SetOriginalBranchName(branch.GetName())
}

// GetModel returns the underlying split model for testing/inspection
func (h *TUISplitHandler) GetModel() *splitcomp.Model {
	return h.model
}
