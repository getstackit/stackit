package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// Prompt notes record why a commit was made. They are a domain concern, so the
// engine owns them: the action layer must not reach through Git() to the raw
// runner (see .claude/rules/package-dependencies.md).

// ShowPromptNote returns the note attached to commit in namespace, or nil.
func (e *engineImpl) ShowPromptNote(ctx context.Context, commit, namespace string) (*git.PromptNote, error) {
	return e.git.ShowPromptNote(ctx, commit, namespace)
}

// AddPromptNote creates or replaces the note on commit in namespace.
func (e *engineImpl) AddPromptNote(ctx context.Context, commit, namespace string, note *git.PromptNote) error {
	return e.git.AddPromptNote(ctx, commit, namespace, note)
}

// LogWithNotes lists commits in base..head along with their notes.
func (e *engineImpl) LogWithNotes(ctx context.Context, base, head, namespace string) ([]git.NoteEntry, error) {
	return e.git.LogWithNotes(ctx, base, head, namespace)
}

// PushNotes pushes the notes refs to the remote.
func (e *engineImpl) PushNotes(ctx context.Context) error {
	return e.git.PushNotes(ctx)
}

// FetchNotes fetches the notes refs from the remote.
func (e *engineImpl) FetchNotes(ctx context.Context) error {
	return e.git.FetchNotes(ctx)
}

// EnsureNotesRewriteConfigured makes git carry notes across commit rewrites.
func (e *engineImpl) EnsureNotesRewriteConfigured() error {
	return e.git.EnsureNotesRewriteConfigured()
}

// EnsureNotesRefspecConfigured adds the notes refspec to the remote config.
func (e *engineImpl) EnsureNotesRefspecConfigured() error {
	return e.git.EnsureNotesRefspecConfigured()
}
