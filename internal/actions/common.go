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

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
)

func printRerereResolved(ctx *app.Context, count int) {
	ctx.Output.Info("%s", restack.FormatRerereResolved(count))
}

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

// SnapshotsFrom builds Snapshots from the command's runtime context.
func SnapshotsFrom(ctx *app.Context) Snapshots {
	return Snapshots{Engine: ctx.Engine, Enabled: ctx.Config == nil || ctx.Config.UndoEnabled(), Log: ctx.Output}
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

// TakeBestEffortSnapshot is the *app.Context form of Snapshots.Take.
func TakeBestEffortSnapshot(ctx *app.Context, opts engine.SnapshotOptions) {
	SnapshotsFrom(ctx).Take(ctx.Context, opts)
}

// MutationDeps are the collaborators every branch-rewriting action shares.
// Embed it in an action's Deps next to the action's own Engine interface.
type MutationDeps struct {
	Out       output.Output
	Here      Here
	Snapshots Snapshots
	Restack   restack.Runner
}

// MutationDepsFrom builds MutationDeps from the command's runtime context.
func MutationDepsFrom(ctx *app.Context) MutationDeps {
	return MutationDeps{
		Out:       ctx.Output,
		Here:      HereFrom(ctx),
		Snapshots: SnapshotsFrom(ctx),
		Restack:   RestackRunner(ctx),
	}
}

// RestackRunner builds the restack runner for the command's runtime context,
// printing per-branch results as plain text.
func RestackRunner(ctx *app.Context) restack.Runner {
	planner, executor := restacker(ctx, nil)
	return restack.Runner{Planner: planner, Executor: executor}
}

// Restacker is a minimal interface needed for restacking branches
type Restacker interface {
	engine.BranchReader
	engine.SyncManager
}

// RestackBranches restacks a list of branches using the engine's batch restack method
func RestackBranches(ctx *app.Context, branches engine.Branches) error {
	return RestackBranchesWithHandler(ctx, branches, nil, restack.ConflictEnterWorkflow)
}

// RestackBranchesWithHandler restacks branches with optional progress callback.
// See restack.ConflictMode for how mode controls conflict handling.
func RestackBranchesWithHandler(ctx *app.Context, branches engine.Branches, callback restack.ProgressFunc, mode restack.ConflictMode) error {
	return restackBranchesWithPlan(ctx, branches, nil, callback, mode, nil)
}

// restacker builds the restack planner and executor for one command run.
// report receives each branch's outcome; nil prints plain text instead.
func restacker(ctx *app.Context, report restack.ProgressFunc) (restack.Planner, restack.Executor) {
	if report == nil {
		report = restack.TextReporter(ctx.Output)
	}
	return restack.Planner{Engine: ctx.Engine, Log: ctx.Logger}, restack.Executor{
		Engine:        ctx.Engine,
		Log:           ctx.Logger,
		Report:        report,
		Continuations: restack.ContinuationFile(ctx.RepoRoot),
		Conflicts:     conflictReporter(ctx),
	}
}

func conflictReporter(ctx *app.Context) *restack.ConflictReporter {
	return &restack.ConflictReporter{Engine: ctx.Engine, Out: ctx.Output, RepoRoot: ctx.RepoRoot}
}

// restackBranchesWithPlan plans and executes a restack of branches. When
// prePlan is non-nil the engine plan is reused rather than recomputed — used
// by RestackAction, which already built it to gate TUI initialization.
// resume is set when continuing an interrupted command.
func restackBranchesWithPlan(ctx *app.Context, branches engine.Branches, prePlan *engine.RestackPlan, report restack.ProgressFunc, mode restack.ConflictMode, resume *config.ContinuationState) error {
	planner, executor := restacker(ctx, report)
	plan, err := planner.Plan(ctx.Context, branches, prePlan)
	if err != nil {
		return err
	}
	return executor.Execute(ctx.Context, plan, mode, resume)
}

// ResolveConflictWorkflow enters the conflict workflow from an interactive
// "resolve conflicts now?" prompt by re-running the conflicted stack in
// restack.ConflictEnterWorkflow. The prompt paths run their initial restack in
// restack.ConflictContinue, which holds back the ENTIRE conflicted stack — the
// conflict branch's ancestors included. Calling EnterConflictWorkflow directly
// from that state would rebase the conflict branch against its never-moved
// parent, find no conflict, and fail after detaching HEAD. Re-running in
// EnterWorkflow mode applies the ancestors first (that mode is exempt from
// per-stack atomicity on purpose) and then enters the conflict on top of them.
// stackBranches must be the conflicted stack in topological order.
func ResolveConflictWorkflow(ctx *app.Context, stackBranches engine.Branches) error {
	return restackBranchesWithPlan(ctx, stackBranches, nil, nil, restack.ConflictEnterWorkflow, nil)
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

// PrintConflictStatus displays conflict information and instructions to the user.
// Assumes a rebase is in progress.
func PrintConflictStatus(ctx *app.Context, branchName string) error {
	return conflictReporter(ctx).Print(ctx.Context, branchName)
}
