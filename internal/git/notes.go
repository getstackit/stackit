package git

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

const promptNotesRef = "refs/notes/prompts"

// PromptNote stores LLM context on a commit via git notes.
type PromptNote struct {
	Prompt    string   `json:"prompt"`
	Summary   string   `json:"summary"`
	Model     string   `json:"model,omitempty"`
	Timestamp string   `json:"timestamp"`
	Memory    []string `json:"memory,omitempty"`
}

// NoteEntry pairs a commit SHA and message with its optional prompt note.
type NoteEntry struct {
	SHA     string
	Subject string
	Note    *PromptNote
}

// AddPromptNote adds or replaces a prompt note on the given commit.
func (r *runner) AddPromptNote(ctx context.Context, commit string, note *PromptNote) error {
	if note.Timestamp == "" {
		note.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("failed to marshal prompt note: %w", err)
	}

	// git notes --ref=refs/notes/prompts add -f -m '<json>' <commit>
	_, err = r.RunGitCommandWithContext(ctx, "notes", "--ref="+promptNotesRef, "add", "-f", "-m", string(data), commit)
	if err != nil {
		return fmt.Errorf("failed to add prompt note: %w", err)
	}
	return nil
}

// ShowPromptNote reads the prompt note for the given commit.
// Returns nil if no note exists.
func (r *runner) ShowPromptNote(ctx context.Context, commit string) (*PromptNote, error) {
	output, err := r.RunGitCommandRawWithContext(ctx, "notes", "--ref="+promptNotesRef, "show", commit)
	if err != nil {
		// No note exists for this commit - git notes show exits non-zero
		return nil, nil //nolint:nilerr // expected: git exits non-zero when no note exists
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var note PromptNote
	if err := json.Unmarshal([]byte(output), &note); err != nil {
		return nil, fmt.Errorf("failed to parse prompt note: %w", err)
	}
	return &note, nil
}

// RemovePromptNote removes the prompt note from the given commit.
func (r *runner) RemovePromptNote(ctx context.Context, commit string) error {
	_, err := r.RunGitCommandRawWithContext(ctx, "notes", "--ref="+promptNotesRef, "remove", commit)
	if err != nil {
		return fmt.Errorf("failed to remove prompt note: %w", err)
	}
	return nil
}

// LogWithNotes returns commits between base..head with their prompt notes.
func (r *runner) LogWithNotes(ctx context.Context, base, head string) ([]NoteEntry, error) {
	// Get commit SHAs and subjects
	shas, err := r.GetCommitRange(ctx, base, head, "%H %s")
	if err != nil {
		return nil, fmt.Errorf("failed to get commit range: %w", err)
	}

	entries := make([]NoteEntry, 0, len(shas))
	for _, line := range shas {
		if line == "" {
			continue
		}
		sha, subject, _ := strings.Cut(line, " ")
		entry := NoteEntry{
			SHA:     sha,
			Subject: subject,
		}
		// Try to read note for this commit
		note, err := r.ShowPromptNote(ctx, sha)
		if err == nil && note != nil {
			entry.Note = note
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// PushNotes pushes prompt notes to the remote.
func (r *runner) PushNotes(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "push", "origin", "+"+promptNotesRef)
	if err != nil {
		return fmt.Errorf("failed to push prompt notes: %w", err)
	}
	return nil
}

// FetchNotes fetches prompt notes from the remote.
func (r *runner) FetchNotes(ctx context.Context) error {
	_, err := r.RunGitCommandRawWithContext(ctx, "fetch", "origin", "+"+promptNotesRef+":"+promptNotesRef)
	if err != nil {
		return fmt.Errorf("failed to fetch prompt notes: %w", err)
	}
	return nil
}

// EnsureNotesRewriteConfigured configures notes.rewriteRef so git
// natively copies prompt notes during rebase.
func (r *runner) EnsureNotesRewriteConfigured() error {
	values, err := r.GetConfigAll("notes.rewriteRef")
	if err != nil {
		// Config key doesn't exist yet, that's fine
		return r.AddConfigValue("notes.rewriteRef", promptNotesRef)
	}

	if slices.Contains(values, promptNotesRef) {
		return nil // Already configured
	}

	return r.AddConfigValue("notes.rewriteRef", promptNotesRef)
}

// EnsureNotesRefspecConfigured adds a fetch refspec for prompt notes
// so that `git fetch` automatically fetches them.
func (r *runner) EnsureNotesRefspecConfigured() error {
	const notesRefspec = "+" + promptNotesRef + ":" + promptNotesRef

	refspecs, err := r.GetConfigAll("remote.origin.fetch")
	if err != nil {
		return fmt.Errorf("failed to get fetch refspecs: %w", err)
	}

	if slices.Contains(refspecs, notesRefspec) {
		return nil // Already configured
	}

	return r.AddConfigValue("remote.origin.fetch", notesRefspec)
}
