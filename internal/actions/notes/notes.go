// Package notes implements the stackit notes command for managing prompt notes on commits.
package notes

import (
	"encoding/json"
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

// ShowOptions contains options for the show subcommand.
type ShowOptions struct {
	Commit string // Commit to show note for (defaults to HEAD)
}

// AddOptions contains options for the add subcommand.
type AddOptions struct {
	Prompt  string
	Summary string
	Model   string
	Memory  []string
	JSON    string // Raw JSON alternative to individual flags
}

// LogOptions contains options for the log subcommand.
type LogOptions struct {
	// Empty for now - uses current branch's commit range
}

// Show displays the prompt note for a commit.
func Show(ctx *app.Context, opts ShowOptions) error {
	out := ctx.Output

	commit := opts.Commit
	if commit == "" {
		rev, err := ctx.Git().GetCurrentRevision(ctx.Context)
		if err != nil {
			return fmt.Errorf("failed to get current revision: %w", err)
		}
		commit = rev
	}

	note, err := ctx.Git().ShowPromptNote(ctx.Context, commit)
	if err != nil {
		return fmt.Errorf("failed to read prompt note: %w", err)
	}

	if note == nil {
		shortSHA := commit
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		out.Info("No prompt note on %s.", style.ColorDim(shortSHA))
		return nil
	}

	printNote(out, note)
	return nil
}

// Add creates or replaces a prompt note on HEAD.
func Add(ctx *app.Context, opts AddOptions) error {
	out := ctx.Output
	g := ctx.Git()

	rev, err := g.GetCurrentRevision(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to get current revision: %w", err)
	}

	var note git.PromptNote

	if opts.JSON != "" {
		if err := json.Unmarshal([]byte(opts.JSON), &note); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		if opts.Prompt == "" {
			return fmt.Errorf("--prompt is required")
		}
		note = git.PromptNote{
			Prompt:  opts.Prompt,
			Summary: opts.Summary,
			Model:   opts.Model,
			Memory:  opts.Memory,
		}
	}

	// Ensure notes.rewriteRef is configured so notes survive rebase
	if err := g.EnsureNotesRewriteConfigured(); err != nil {
		out.Debug("Failed to configure notes.rewriteRef: %v", err)
	}

	if err := g.AddPromptNote(ctx.Context, rev, &note); err != nil {
		return err
	}

	shortSHA := rev
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}
	out.Info("Added prompt note to %s.", style.ColorDim(shortSHA))
	return nil
}

// Log shows commits on the current branch with their prompt notes.
func Log(ctx *app.Context) error {
	eng := ctx.Engine
	out := ctx.Output

	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	// Get parent branch for commit range
	parent := currentBranch.GetParent()
	if parent == nil {
		return fmt.Errorf("branch %s has no parent; cannot determine commit range", currentBranch.GetName())
	}

	entries, err := ctx.Git().LogWithNotes(ctx.Context, parent.GetName(), currentBranch.GetName())
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		out.Info("No commits between %s and %s.", parent.GetName(), currentBranch.GetName())
		return nil
	}

	for _, entry := range entries {
		shortSHA := entry.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		out.Info("%s %s", style.ColorDim(shortSHA), entry.Subject)
		if entry.Note != nil {
			out.Info("  Prompt:  %s", entry.Note.Prompt)
			if entry.Note.Summary != "" {
				out.Info("  Summary: %s", entry.Note.Summary)
			}
		} else {
			out.Info("  %s", style.ColorDim("(no prompt note)"))
		}
	}

	return nil
}

// Push pushes prompt notes to the remote.
func Push(ctx *app.Context) error {
	out := ctx.Output

	if err := ctx.Git().PushNotes(ctx.Context); err != nil {
		return err
	}

	out.Info("Pushed prompt notes to remote.")
	return nil
}

func printNote(out output.Output, note *git.PromptNote) {
	out.Info("Prompt:  %s", note.Prompt)
	if note.Summary != "" {
		out.Info("Summary: %s", note.Summary)
	}
	if note.Model != "" {
		out.Info("Model:   %s", note.Model)
	}
	if len(note.Memory) > 0 {
		out.Info("Memory:")
		for _, m := range note.Memory {
			out.Info("  - %s", m)
		}
	}
}
