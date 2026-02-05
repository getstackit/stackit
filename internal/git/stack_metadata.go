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
	"encoding/json"
	"fmt"
	"strings"
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

// ReadStackMeta reads stack metadata for a given stack ID.
// Returns nil with no error if the stack doesn't exist.
func (r *runner) ReadStackMeta(stackID string) (*StackMeta, error) {
	refName := StackMetaRefName(stackID)

	sha, err := r.GetRef(refName)
	if err != nil {
		// If ref doesn't exist, it's not an error, just means no metadata
		return nil, nil //nolint:nilerr
	}

	content, err := r.ReadBlob(sha)
	if err != nil {
		return nil, fmt.Errorf("failed to read stack metadata blob %s: %w", sha, err)
	}

	if content == "" {
		return nil, nil
	}

	var meta StackMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stack metadata for %s: %w", stackID, err)
	}

	return &meta, nil
}

// WriteStackMeta writes stack metadata for a given stack ID.
func (r *runner) WriteStackMeta(stackID string, meta *StackMeta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal stack metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create stack metadata blob: %w", err)
	}

	if err := r.UpdateRef(StackMetaRefName(stackID), sha); err != nil {
		return fmt.Errorf("failed to write stack metadata ref: %w", err)
	}

	return nil
}

// DeleteStackMeta deletes stack metadata for a given stack ID.
func (r *runner) DeleteStackMeta(stackID string) error {
	return r.DeleteRef(StackMetaRefName(stackID))
}

// ListStackMetas returns a map of stack IDs to their ref SHAs.
func (r *runner) ListStackMetas() (map[string]string, error) {
	refs, err := r.ListRefs(StackMetaRefPrefix)
	if err != nil {
		return nil, err
	}

	// Remove prefix from stack IDs
	result := make(map[string]string)
	for refName, sha := range refs {
		stackID := strings.TrimPrefix(refName, StackMetaRefPrefix)
		result[stackID] = sha
	}
	return result, nil
}

// WriteStackMetaBlob creates a blob containing the stack metadata JSON and returns its SHA.
// This does NOT update any refs - use this for batched/transactional writes.
func (r *runner) WriteStackMetaBlob(meta *StackMeta) (string, error) {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal stack metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create stack metadata blob: %w", err)
	}

	return sha, nil
}

// GetStackMetaRefSHA returns the current SHA of a stack metadata ref, or empty string if not found.
func (r *runner) GetStackMetaRefSHA(stackID string) string {
	sha, err := r.GetRef(StackMetaRefName(stackID))
	if err != nil {
		return ""
	}
	return sha
}

// StackMetaRefName returns the full ref name for a stack's metadata.
// Use this helper instead of concatenating StackMetaRefPrefix directly
// to ensure consistent ref name construction across all stack metadata operations.
func StackMetaRefName(stackID string) string {
	return StackMetaRefPrefix + stackID
}
