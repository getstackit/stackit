package stack

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/track"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// answerYes is the prompt value returned when the user selects "Yes".
const answerYes = "yes"

// NewTrackUI creates a handler for track operations.
// Caller must defer handler.Cleanup() to restore terminal on exit.
func NewTrackUI(out output.Output, interactive bool) track.Handler {
	if interactive {
		return NewInteractiveTrackHandler(out)
	}
	return NewSimpleTrackHandler(out)
}

// SimpleTrackHandler provides non-interactive handling for track operations
type SimpleTrackHandler struct {
	common.BaseHandler
}

// NewSimpleTrackHandler creates a new SimpleTrackHandler
func NewSimpleTrackHandler(out output.Output) *SimpleTrackHandler {
	return &SimpleTrackHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// PromptSelectParent returns empty string for simple handler (non-interactive)
func (h *SimpleTrackHandler) PromptSelectParent(_ context.Context, _ engine.Engine, _ github.Client, _ output.Logger, _ string) (string, error) {
	return "", fmt.Errorf("parent selection requires interactive mode; use --parent or --force")
}

// PromptTrackChild returns false for simple handler (auto-skip)
func (h *SimpleTrackHandler) PromptTrackChild(_, _ string) (bool, error) {
	return false, nil
}

// InteractiveTrackHandler provides interactive prompts for track operations
type InteractiveTrackHandler struct {
	SimpleTrackHandler
}

// NewInteractiveTrackHandler creates a new InteractiveTrackHandler
func NewInteractiveTrackHandler(out output.Output) *InteractiveTrackHandler {
	return &InteractiveTrackHandler{
		SimpleTrackHandler: *NewSimpleTrackHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveTrackHandler) IsInteractive() bool {
	return true
}

// PromptSelectParent prompts user to select a parent for the branch
func (h *InteractiveTrackHandler) PromptSelectParent(ctx context.Context, eng engine.Engine, ghClient github.Client, logger output.Logger, branchName string) (string, error) {
	// Show interactive selector
	selected, err := tui.PromptTreeSelect(ctx, eng, ghClient, tui.TreeOptions{
		Style:  "FULL",
		Logger: logger,
		Exclude: map[string]bool{
			branchName: true,
		},
	})
	if err != nil {
		return "", err
	}

	return selected, nil
}

// PromptTrackChild prompts user to confirm tracking a child branch
func (h *InteractiveTrackHandler) PromptTrackChild(childName, parentName string) (bool, error) {
	message := fmt.Sprintf("Found untracked child branch %s of %s. Track it?", style.ColorBranchName(childName), style.ColorBranchName(parentName))
	options := []tui.SelectOption{
		{Label: "Yes", Value: answerYes},
		{Label: "No", Value: "no"},
	}

	selected, err := tui.PromptSelect(message, options, 0)
	if err != nil {
		return false, err
	}

	return selected == answerYes, nil
}
