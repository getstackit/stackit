// Package split provides a unified TUI component for split operations.
package split

import (
	"github.com/getstackit/stackit/internal/git"
)

// TypeSelectedMsg indicates user selected a split type
type TypeSelectedMsg struct {
	Style Style
}

// DirectionSelectedMsg indicates user selected a direction
type DirectionSelectedMsg struct {
	Direction Direction
}

// HunksLoadedMsg indicates hunks have been loaded for selection
type HunksLoadedMsg struct {
	Hunks []git.Hunk
	Diff  string
}

// HunksSelectedMsg indicates user finished selecting hunks
type HunksSelectedMsg struct {
	Hunks []git.Hunk
}

// BranchNameEnteredMsg indicates user entered a branch name
type BranchNameEnteredMsg struct {
	Name string
}

// EditMessageConfirmedMsg indicates user responded to edit message prompt
type EditMessageConfirmedMsg struct {
	WantsEdit bool
}

// EditorRequestMsg indicates the model needs an external editor
type EditorRequestMsg struct {
	DefaultMessage string
}

// EditorCompleteMsg indicates external editor completed
type EditorCompleteMsg struct {
	Message string
	Error   error
}

// BranchCreatedMsg indicates a branch was created
type BranchCreatedMsg struct {
	Name string
}

// LoopContinueMsg indicates the hunk loop should continue
type LoopContinueMsg struct{}

// CompleteMsg indicates split is complete
type CompleteMsg struct {
	Branches []string
}

// CancelMsg indicates user canceled the operation
type CancelMsg struct{}

// ErrorMsg indicates an error occurred
type ErrorMsg struct {
	Error error
}

// NoChangesMsg indicates no changes were staged
type NoChangesMsg struct{}

// RetryMsg indicates user wants to retry hunk selection
type RetryMsg struct{}
