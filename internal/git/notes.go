package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const promptNotesRef = "refs/notes/prompts"
const DefaultNotesNamespace = "story"

// PromptNote stores contextual metadata on a commit via git notes.
type PromptNote struct {
	Prompt    string   `json:"prompt"`
	Summary   string   `json:"summary"`
	Model     string   `json:"model,omitempty"`
	Timestamp string   `json:"timestamp"`
	Memory    []string `json:"memory,omitempty"`
}

// noteEnvelope stores namespaced notes inside a single git note payload.
type noteEnvelope struct {
	Namespaces map[string]PromptNote `json:"namespaces"`
}

// NoteEntry pairs a commit SHA and message with its optional note.
type NoteEntry struct {
	SHA     string
	Subject string
	Note    *PromptNote
}

// AddPromptNote adds or replaces a note in the given namespace on the commit.
func (r *runner) AddPromptNote(ctx context.Context, commit, namespace string, note *PromptNote) error {
	ns, err := normalizeNoteNamespace(namespace)
	if err != nil {
		return err
	}

	if note.Timestamp == "" {
		note.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	env, err := r.readNoteEnvelope(ctx, commit)
	if err != nil {
		return err
	}
	if env == nil {
		env = &noteEnvelope{Namespaces: map[string]PromptNote{}}
	}
	if env.Namespaces == nil {
		env.Namespaces = map[string]PromptNote{}
	}
	env.Namespaces[ns] = *note

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("failed to marshal note envelope: %w", err)
	}

	// git notes --ref=refs/notes/prompts add -f -m '<json>' <commit>
	_, err = r.RunGitCommandWithContext(ctx, "notes", "--ref="+promptNotesRef, "add", "-f", "-m", string(data), commit)
	if err != nil {
		return fmt.Errorf("failed to add note: %w", err)
	}
	return nil
}

// ShowPromptNote reads a note in the given namespace for the commit.
// Returns nil if no note exists.
func (r *runner) ShowPromptNote(ctx context.Context, commit, namespace string) (*PromptNote, error) {
	ns, err := normalizeNoteNamespace(namespace)
	if err != nil {
		return nil, err
	}

	env, err := r.readNoteEnvelope(ctx, commit)
	if err != nil {
		return nil, err
	}
	if env == nil || len(env.Namespaces) == 0 {
		return nil, nil
	}

	note, ok := env.Namespaces[ns]
	if !ok {
		return nil, nil
	}
	return &note, nil
}

// RemovePromptNote removes a note in the given namespace from the commit.
func (r *runner) RemovePromptNote(ctx context.Context, commit, namespace string) error {
	ns, err := normalizeNoteNamespace(namespace)
	if err != nil {
		return err
	}

	env, err := r.readNoteEnvelope(ctx, commit)
	if err != nil {
		return err
	}
	if env == nil || len(env.Namespaces) == 0 {
		return nil
	}

	delete(env.Namespaces, ns)
	if len(env.Namespaces) == 0 {
		_, err := r.RunGitCommandRawWithContext(ctx, "notes", "--ref="+promptNotesRef, "remove", commit)
		if err != nil && !isNoteMissingError(err) {
			return fmt.Errorf("failed to remove note: %w", err)
		}
		return nil
	}

	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("failed to marshal note envelope: %w", err)
	}

	_, err = r.RunGitCommandWithContext(ctx, "notes", "--ref="+promptNotesRef, "add", "-f", "-m", string(data), commit)
	if err != nil {
		return fmt.Errorf("failed to update note: %w", err)
	}
	return nil
}

// LogWithNotes returns commits between base..head with notes for the namespace.
func (r *runner) LogWithNotes(ctx context.Context, base, head, namespace string) ([]NoteEntry, error) {
	ns, err := normalizeNoteNamespace(namespace)
	if err != nil {
		return nil, err
	}

	shas, err := r.GetCommitRange(ctx, base, head, "SHA")
	if err != nil {
		return nil, fmt.Errorf("failed to get commit range: %w", err)
	}

	entries := make([]NoteEntry, 0, len(shas))
	for _, sha := range shas {
		if sha == "" {
			continue
		}
		subject, err := r.GetCommitLog(sha, "%s")
		if err != nil {
			return nil, fmt.Errorf("failed to get subject for %s: %w", sha, err)
		}
		entry := NoteEntry{
			SHA:     sha,
			Subject: subject,
		}
		note, err := r.ShowPromptNote(ctx, sha, ns)
		if err != nil {
			return nil, err
		}
		if note != nil {
			entry.Note = note
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// PushNotes pushes notes to the remote.
func (r *runner) PushNotes(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "push", "origin", "+"+promptNotesRef)
	if err != nil {
		return fmt.Errorf("failed to push notes: %w", err)
	}
	return nil
}

// FetchNotes fetches notes from the remote.
func (r *runner) FetchNotes(ctx context.Context) error {
	_, err := r.RunGitCommandRawWithContext(ctx, "fetch", "origin", "+"+promptNotesRef+":"+promptNotesRef)
	if err != nil {
		return fmt.Errorf("failed to fetch notes: %w", err)
	}
	return nil
}

// EnsureNotesRewriteConfigured configures notes.rewriteRef so git
// natively copies notes during rebase.
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

func normalizeNoteNamespace(namespace string) (string, error) {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return DefaultNotesNamespace, nil
	}
	if strings.Contains(ns, "/") {
		return "", fmt.Errorf("invalid namespace %q: '/' is not allowed", ns)
	}
	return ns, nil
}

func (r *runner) readNoteEnvelope(ctx context.Context, commit string) (*noteEnvelope, error) {
	output, err := r.RunGitCommandRawWithContext(ctx, "notes", "--ref="+promptNotesRef, "show", commit)
	if err != nil {
		if isNoteMissingError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read note: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}
	return decodeNoteEnvelope([]byte(output))
}

func decodeNoteEnvelope(data []byte) (*noteEnvelope, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("failed to parse note payload: %w", err)
	}

	// Envelope format: {"namespaces":{"story":{...}}}
	if _, ok := probe["namespaces"]; ok {
		var env noteEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("failed to parse note envelope: %w", err)
		}
		if env.Namespaces == nil {
			env.Namespaces = map[string]PromptNote{}
		}
		return &env, nil
	}

	// Backward compatibility for legacy single-note payload.
	var legacy PromptNote
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("failed to parse legacy note payload: %w", err)
	}
	return &noteEnvelope{
		Namespaces: map[string]PromptNote{
			DefaultNotesNamespace: legacy,
		},
	}, nil
}

func isNoteMissingError(err error) bool {
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		return false
	}
	return strings.Contains(cmdErr.Stderr, "no note found for object")
}
