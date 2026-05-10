package split

import "github.com/getstackit/stackit/internal/engine"

// Style specifies the split mode
// Note: This mirrors split.Style from actions/split to avoid import cycles.
// Callers convert between the two types.
type Style string

// Style constants
const (
	StyleCommit Style = "commit"
	StyleHunk   Style = "hunk"
	StyleFile   Style = "file"
)

// Direction specifies where the new branch should be placed
// Note: This mirrors split.Direction from actions/split to avoid import cycles.
type Direction string

// Direction constants
const (
	DirectionBelow Direction = "below"
	DirectionAbove Direction = "above"
)

// TypeChoice represents an available split type option
type TypeChoice struct {
	Style       Style
	Label       string
	Description string
	Available   bool
}

// DirectionContext provides context for the direction selection
type DirectionContext struct {
	Engine        engine.BranchReader
	CurrentBranch string
	ParentBranch  string
	Children      []string
}

// Config provides configuration for the split model
type Config struct {
	Engine engine.Engine
	Branch engine.Branch

	// PreselectedStyle skips type selection if set
	PreselectedStyle Style
	// PreselectedDirection skips direction selection if set
	PreselectedDirection Direction

	// UseGitAddP uses git add -p instead of TUI hunk selector
	UseGitAddP bool

	// AvailableTypes provides the type choices when style not preselected
	AvailableTypes []TypeChoice
}

// Result contains the result of a split operation
type Result struct {
	Branches  []string
	Canceled  bool
	Error     error
	Style     Style
	Direction Direction
}
