package split

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// DirectionContext provides context for the direction selection prompt
type DirectionContext struct {
	Engine        engine.BranchReader
	CurrentBranch string
	ParentBranch  string
	Children      []string
}

// CommitMessageContext provides context for commit message prompting
type CommitMessageContext struct {
	// Files being extracted to the new branch
	Files []string
	// Direction of the split (above = child, below = parent)
	Direction Direction
	// CurrentBranch is the name of the branch being split
	CurrentBranch string
	// OriginalCommitMessage is the full message of the first commit on the branch
	// (title + body). Used as the basis for the split commit message.
	OriginalCommitMessage string
}

// TypeChoice represents an available split type option
type TypeChoice struct {
	Style       Style
	Label       string
	Description string
	Available   bool // false if the option is not available (e.g., commit split for single-commit branch)
}

// InteractiveHandler extends Handler with interactive prompt capabilities.
// This allows the action layer to request user input without depending on
// specific UI implementations (TUI, CLI, etc.).
//
// Error handling: Prompt methods should return sterrors.ErrCanceled when the
// user cancels (e.g., Ctrl+C, Escape). Other errors indicate actual failures.
type InteractiveHandler interface {
	Handler

	// IsInteractive returns true if this handler supports interactive prompts.
	// Non-interactive handlers return false, and their prompt methods will return errors.
	IsInteractive() bool

	// PromptSplitType asks the user to choose between available split types.
	// availableTypes contains the choices with their availability status.
	// Returns the selected Style or an error if canceled.
	PromptSplitType(availableTypes []TypeChoice) (Style, error)

	// PromptDirection asks the user where to place the new branch.
	// Returns the selected Direction or an error if canceled.
	PromptDirection(ctx DirectionContext) (Direction, error)

	// ShowHunkSummary displays a summary of the remaining changes before staging.
	// This is informational only, no user input required.
	ShowHunkSummary(diff string)

	// PromptCommitMessage asks the user to enter or edit a commit message.
	// defaultMsg is the suggested default (e.g., from original commit).
	// Returns the final message or an error if canceled.
	PromptCommitMessage(defaultMsg string) (string, error)

	// PromptCommitMessageWithContext asks the user to enter a commit message
	// with full context about the split operation displayed.
	// The context includes files being extracted, direction, and current branch.
	// Returns the commit message or an error if canceled.
	PromptCommitMessageWithContext(ctx CommitMessageContext) (string, error)

	// PromptBranchName asks the user to enter a branch name.
	// defaultName is the auto-generated suggestion.
	// sessionNames contains names already used in this split session.
	// allBranchNames contains all existing branch names in the repo (for O(1) lookup).
	// originalBranchName is the name of the branch being split (allowed to reuse).
	// Returns the final name or an error if canceled.
	PromptBranchName(defaultName string, sessionNames []string, allBranchNames *engine.BranchSet, originalBranchName string) (string, error)

	// PromptContinueOrCancel asks user whether to continue after no changes were staged.
	// Returns true to try again, false to cancel.
	PromptContinueOrCancel() (bool, error)

	// PromptEditCommitMessage asks whether the user wants to edit the commit message.
	// Returns true if user wants to edit.
	PromptEditCommitMessage() (bool, error)

	// PromptSelectHunks displays the hunk selector TUI and returns selected hunks.
	// Returns the hunks that the user selected, or an error if canceled.
	PromptSelectHunks(hunks []git.Hunk) ([]git.Hunk, error)
}
