package git

// Stack metadata is stored in Git refs at refs/stackit/stacks/{stack-id}.
// This is separate from branch metadata (refs/stackit/metadata/{branch})
// and survives branch operations like merging the root branch.
//
// Ref namespaces used by stackit:
//   - refs/stackit/metadata/     - Per-branch metadata (parent, PR info, etc.)
//   - refs/stackit/local-metadata/ - Local-only branch metadata (never pushed)
//   - refs/stackit/stacks/       - Stack-level metadata (title, description, etc.)
//   - refs/stackit/remote-stacks/ - Fetched remote stack metadata

import (
	"time"
)

// StackMeta represents stack-level metadata stored in Git refs.
// This is separate from branch metadata (Meta) and survives branch operations
// like merging the root branch.
type StackMeta struct {
	ID          string    `json:"id"`                    // Matches ref name (timestamp-sanitized-root)
	Title       string    `json:"title,omitempty"`       // Stack title
	Description string    `json:"description,omitempty"` // Stack description
	CreatedAt   time.Time `json:"createdAt"`             // When stack was created
	CreatedBy   string    `json:"createdBy,omitempty"`   // Who created the stack (git user)
}

// StackDescription returns a StackDescription from the StackMeta fields.
// This provides compatibility with the existing StackDescription type.
func (sm *StackMeta) StackDescription() *StackDescription {
	if sm == nil || (sm.Title == "" && sm.Description == "") {
		return nil
	}
	return &StackDescription{
		Title:       sm.Title,
		Description: sm.Description,
	}
}

// IsEmpty returns true if both title and description are empty.
func (sm *StackMeta) IsEmpty() bool {
	return sm.StackDescription() == nil
}

const (
	// StackMetaRefPrefix is the prefix for Git refs where stack metadata is stored
	StackMetaRefPrefix = "refs/stackit/stacks/"
	// RemoteStackMetaRefPrefix is the prefix for remote stack metadata refs (fetched from remote)
	RemoteStackMetaRefPrefix = "refs/stackit/remote-stacks/"
)

// StackMetaRefName returns the full ref name for a stack's metadata.
// Use this helper instead of concatenating StackMetaRefPrefix directly
// to ensure consistent ref name construction across all stack metadata operations.
func StackMetaRefName(stackID string) string {
	return StackMetaRefPrefix + stackID
}
