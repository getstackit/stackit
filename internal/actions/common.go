// Package actions holds the reusable operations behind stackit's commands
// (create, submit, sync, ...), orchestrating the engine, git and GitHub
// layers. Interaction goes through prompt and handler interfaces that the
// CLI and TUI adapters implement.
//
// Operations still being migrated take *app.Context; converted ones live in
// subpackages and take a context.Context plus a Deps struct of narrow
// interfaces (see docs/architecture.md).
package actions

import (
	"context"

	"github.com/getstackit/stackit/internal/actions/restack"
	"github.com/getstackit/stackit/internal/output"

	"github.com/getstackit/stackit/internal/engine"
)

// SnapshotEngine is the engine surface undo snapshots use.
type SnapshotEngine interface {
	TakeSnapshot(ctx context.Context, opts engine.SnapshotOptions) error
	ClearLastSnapshotID()
}

// Snapshots records the undo snapshot a mutating command takes before it
// changes anything.
type Snapshots struct {
	Engine SnapshotEngine
	// Enabled reflects undo.enabled.
	Enabled bool
	Log     DebugLogger
}

// Take records an undo snapshot and logs failures without aborting the
// action. When undo is disabled, it clears the rollback binding without
// writing a snapshot.
func (s Snapshots) Take(ctx context.Context, opts engine.SnapshotOptions) {
	if !s.Enabled {
		s.Engine.ClearLastSnapshotID()
		return
	}
	if err := s.Engine.TakeSnapshot(ctx, opts); err != nil {
		s.Log.Debug("Failed to take snapshot: %v", err)
	}
}

// MutationDeps are the collaborators every branch-rewriting action shares.
// Embed it in an action's Deps next to the action's own Engine interface.
type MutationDeps struct {
	Out       output.Output
	Here      Here
	Snapshots Snapshots
	Restack   restack.Runner
}

// wordBranch is the pluralizable noun passed to PluralSuffix/Pluralize by
// callers describing a count of branches.
const wordBranch = "branch"

// PluralSuffix returns the appropriate plural suffix for the given word if plural is true, otherwise empty string
func PluralSuffix(word string, plural bool) string {
	if !plural {
		return ""
	}
	switch word {
	case wordBranch:
		return "es"
	case "PR":
		return "s"
	default:
		return "s"
	}
}

// Pluralize returns the plural form of word if count != 1, otherwise returns the singular form
func Pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + PluralSuffix(word, true)
}

// PluralIt returns "them" if plural is true, otherwise "it"
func PluralIt(plural bool) string {
	if plural {
		return "them"
	}
	return "it"
}

// SnapshotOption is a function that modifies SnapshotOptions
type SnapshotOption func(*engine.SnapshotOptions)

// NewSnapshot creates a new SnapshotOptions with the given command and options
func NewSnapshot(command string, options ...SnapshotOption) engine.SnapshotOptions {
	opts := engine.SnapshotOptions{
		Command: command,
		Args:    []string{},
	}
	for _, option := range options {
		option(&opts)
	}
	return opts
}

// WithWorktreeCapture records uncommitted changes with the snapshot, so a later
// abort or undo can hand them back. Use it on commands that turn the working
// tree into a commit — modify, create, absorb, split — and nowhere else.
//
// Pick the cheapest level that covers what this invocation stages. Only
// staging everything (`git add -A`) turns an untracked file into a commit;
// `-u` and `-p` never touch untracked files, and anything the user staged
// themselves is already in the tracked (stash) capture. The untracked level
// hashes every untracked file, so it is wasted anywhere else.
func WithWorktreeCapture(capture engine.WorktreeCapture) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		opts.Capture = capture
	}
}

// WithArg appends a single argument if it's not empty
func WithArg(arg string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if arg != "" {
			opts.Args = append(opts.Args, arg)
		}
	}
}

// WithArgs appends multiple arguments
func WithArgs(args ...string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		opts.Args = append(opts.Args, args...)
	}
}

// WithFlag appends a flag if condition is true
func WithFlag(condition bool, flag string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if condition {
			opts.Args = append(opts.Args, flag)
		}
	}
}

// WithFlagValue appends a flag with a value if the value is not empty
func WithFlagValue(flag string, value string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if value != "" {
			opts.Args = append(opts.Args, flag, value)
		}
	}
}
