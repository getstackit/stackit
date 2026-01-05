// Package engine provides undo/redo functionality through state snapshots
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/timeutil"
)

const (
	// DefaultMaxUndoStackDepth is the default number of snapshots we keep
	DefaultMaxUndoStackDepth = 10
	// UndoDir is the directory where undo snapshots are stored
	UndoDir = ".git/stackit/undo"
	// jsonExt is the file extension for snapshot files
	jsonExt = ".json"
)

// Snapshot represents a saved state of the repository
type Snapshot struct {
	Timestamp     time.Time         `json:"timestamp"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	CurrentBranch string            `json:"current_branch"`
	BranchSHAs    map[string]string `json:"branch_shas"`   // branch name -> SHA
	MetadataSHAs  map[string]string `json:"metadata_shas"` // branch name -> metadata ref SHA
}

// SnapshotInfo provides metadata about a snapshot for display
type SnapshotInfo struct {
	ID          string    // Filename without extension
	Command     string    // Command name
	Args        []string  // Command arguments
	Timestamp   time.Time // When the snapshot was taken
	DisplayName string    // Human-readable description
}

// SnapshotOptions contains options for taking a snapshot
type SnapshotOptions struct {
	Command string
	Args    []string
}

// getUndoDir returns the path to the undo directory
func getUndoDir(repoRoot string) string {
	return filepath.Join(repoRoot, UndoDir)
}

// ensureUndoDir creates the undo directory if it doesn't exist
func ensureUndoDir(repoRoot string) error {
	dir := getUndoDir(repoRoot)
	return os.MkdirAll(dir, 0750)
}

// getSnapshotFilename generates a filename for a snapshot
func getSnapshotFilename(timestamp time.Time, command string) string {
	// Format: YYYYMMDDHHMMSS_command.json
	// This ensures chronological ordering when sorted by filename
	return fmt.Sprintf("%s_%s.json", timestamp.Format("20060102150405.000"), command)
}

// parseSnapshotFilename extracts timestamp and command from a filename
func parseSnapshotFilename(filename string) (time.Time, string, error) {
	// Remove .json extension
	if len(filename) < len(jsonExt)+1 || filename[len(filename)-len(jsonExt):] != jsonExt {
		return time.Time{}, "", fmt.Errorf("invalid snapshot filename: %s", filename)
	}
	base := filename[:len(filename)-len(jsonExt)]

	// Split on last underscore
	lastUnderscore := -1
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '_' {
			lastUnderscore = i
			break
		}
	}
	if lastUnderscore == -1 {
		return time.Time{}, "", fmt.Errorf("invalid snapshot filename format: %s", filename)
	}

	timestampStr := base[:lastUnderscore]
	command := base[lastUnderscore+1:]

	timestamp, err := time.Parse("20060102150405.000", timestampStr)
	if err != nil {
		// Try without milliseconds for backward compatibility
		var err2 error
		timestamp, err2 = time.Parse("20060102150405", timestampStr)
		if err2 != nil {
			return time.Time{}, "", fmt.Errorf("failed to parse timestamp: %w", err)
		}
	}

	return timestamp, command, nil
}

// TakeSnapshot captures the current state of the repository
func (e *engineImpl) TakeSnapshot(opts SnapshotOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Ensure undo directory exists
	if err := ensureUndoDir(e.repoRoot); err != nil {
		return fmt.Errorf("failed to create undo directory: %w", err)
	}

	// Get current branch
	currentBranch := e.currentBranch

	// Get all branch SHAs
	branchSHAs := make(map[string]string)
	for _, branchName := range e.branches {
		branch := e.GetBranch(branchName)
		sha, err := branch.GetRevision()
		if err != nil {
			// Skip branches that can't be resolved (might be deleted)
			continue
		}
		branchSHAs[branchName] = sha
	}

	// Get all metadata ref SHAs
	metadataRefs, err := e.git.ListMetadata()
	if err != nil {
		// If we can't get metadata refs, continue with empty map
		metadataRefs = make(map[string]string)
	}

	// Convert metadata refs to branch name -> SHA mapping
	metadataSHAs := make(map[string]string)
	for branchName, sha := range metadataRefs {
		metadataSHAs[branchName] = sha
	}

	// Create snapshot
	timestamp := time.Now()
	snapshot := &Snapshot{
		Timestamp:     timestamp,
		Command:       opts.Command,
		Args:          opts.Args,
		CurrentBranch: currentBranch,
		BranchSHAs:    branchSHAs,
		MetadataSHAs:  metadataSHAs,
	}

	// Serialize to JSON
	jsonData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to file
	filename := getSnapshotFilename(timestamp, opts.Command)
	filePath := filepath.Join(getUndoDir(e.repoRoot), filename)
	if err := os.WriteFile(filePath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Enforce max stack depth by removing oldest snapshots
	if err := e.enforceMaxStackDepth(); err != nil {
		// Log but don't fail - snapshot was already saved
		// We'll just have more than the max snapshots
		_ = err
	}

	return nil
}

// enforceMaxStackDepth removes the oldest snapshots if we exceed MaxUndoStackDepth
func (e *engineImpl) enforceMaxStackDepth() error {
	dir := getUndoDir(e.repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read undo directory: %w", err)
	}

	// Filter to only .json files
	var snapshots []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			snapshots = append(snapshots, entry)
		}
	}

	// If we're under the limit, nothing to do
	if len(snapshots) <= e.maxUndoStackDepth {
		return nil
	}

	// Sort by filename (which includes timestamp, so chronological)
	slices.SortFunc(snapshots, func(a, b os.DirEntry) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})

	// Delete oldest snapshots
	toDelete := len(snapshots) - e.maxUndoStackDepth
	for i := 0; i < toDelete; i++ {
		filePath := filepath.Join(dir, snapshots[i].Name())
		if err := os.Remove(filePath); err != nil {
			// Continue deleting others even if one fails
			continue
		}
	}

	return nil
}

// GetSnapshots returns a list of all available snapshots, sorted by time (newest first)
func (e *engineImpl) GetSnapshots() ([]SnapshotInfo, error) {
	dir := getUndoDir(e.repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SnapshotInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read undo directory: %w", err)
	}

	snapshots := make([]SnapshotInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != jsonExt {
			continue
		}

		// Parse filename to get timestamp and command
		timestamp, command, err := parseSnapshotFilename(entry.Name())
		if err != nil {
			// Skip invalid filenames
			continue
		}

		// Read the snapshot to get args
		filePath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var snapshot Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			continue
		}

		// Generate display name
		displayName := formatSnapshotDisplay(command, snapshot.Args, timestamp)

		snapshots = append(snapshots, SnapshotInfo{
			ID:          entry.Name()[:len(entry.Name())-len(jsonExt)], // Remove .json
			Command:     command,
			Args:        snapshot.Args,
			Timestamp:   timestamp,
			DisplayName: displayName,
		})
	}

	// Sort by timestamp (newest first)
	slices.SortFunc(snapshots, func(a, b SnapshotInfo) int {
		if !a.Timestamp.Equal(b.Timestamp) {
			if a.Timestamp.After(b.Timestamp) {
				return -1 // a is newer, should come first
			}
			return 1 // b is newer
		}
		// Tie-breaker: use ID (filename) descending
		if a.ID > b.ID {
			return -1
		}
		if a.ID < b.ID {
			return 1
		}
		return 0
	})

	return snapshots, nil
}

// formatSnapshotDisplay creates a human-readable description of a snapshot
func formatSnapshotDisplay(command string, args []string, timestamp time.Time) string {
	timeStr := timeutil.FormatTimeAgo(timestamp)

	// Format command with args
	cmdStr := command
	if len(args) > 0 {
		// Limit args display to first 2 for brevity
		displayArgs := args
		if len(displayArgs) > 2 {
			displayArgs = displayArgs[:2]
		}
		cmdStr = fmt.Sprintf("%s %s", command, fmt.Sprint(displayArgs))
	}

	return fmt.Sprintf("Before '%s' (%s)", cmdStr, timeStr)
}

// LoadSnapshot loads a snapshot by ID (filename without .json)
func (e *engineImpl) LoadSnapshot(snapshotID string) (*Snapshot, error) {
	dir := getUndoDir(e.repoRoot)
	filePath := filepath.Join(dir, snapshotID+jsonExt)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot: %w", err)
	}

	return &snapshot, nil
}

// RestoreSnapshot restores the repository to the state captured in a snapshot
func (e *engineImpl) RestoreSnapshot(ctx context.Context, snapshotID string) error {
	// Load the snapshot
	snapshot, err := e.LoadSnapshot(snapshotID)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Get current branches
	currentBranches, err := e.git.GetAllBranchNames()
	if err != nil {
		return fmt.Errorf("failed to get current branches: %w", err)
	}

	// Identify branches to delete (branches that exist now but not in snapshot)
	branchesToDelete := make(map[string]bool)
	for _, branchName := range currentBranches {
		if _, exists := snapshot.BranchSHAs[branchName]; !exists {
			// Don't delete trunk
			if branchName != e.trunk {
				branchesToDelete[branchName] = true
			}
		}
	}

	// Delete branches that were created after the snapshot
	// Get trunk name while holding lock to avoid deadlock
	trunkName := e.trunk
	for branchName := range branchesToDelete {
		// If we're on this branch, switch to trunk first
		if branchName == e.currentBranch {
			// Access trunk directly while holding the lock (avoid deadlock from e.Trunk() trying to acquire RLock)
			trunkBranch := NewBranch(trunkName, e)
			if err := e.git.CheckoutBranch(ctx, trunkBranch.GetName()); err != nil {
				return fmt.Errorf("failed to switch to trunk before deleting branch: %w", err)
			}
			e.currentBranch = trunkName
		}
		// Delete the branch
		branch := e.GetBranch(branchName)
		if err := e.git.DeleteBranch(ctx, branch.GetName()); err != nil {
			// Log but continue - branch might not exist or might be protected
			continue
		}
	}

	// Collect all ref updates for atomic restore
	updates := make([]git.RefUpdate, 0, len(snapshot.BranchSHAs)+len(snapshot.MetadataSHAs))

	for branchName, sha := range snapshot.BranchSHAs {
		updates = append(updates, git.RefUpdate{
			RefName: fmt.Sprintf("refs/heads/%s", branchName),
			NewSHA:  sha,
		})
	}

	for branchName, sha := range snapshot.MetadataSHAs {
		updates = append(updates, git.RefUpdate{
			RefName: fmt.Sprintf("refs/stackit/metadata/%s", branchName),
			NewSHA:  sha,
		})
	}

	// Atomic restore of all refs
	reflogMessage := fmt.Sprintf("stackit undo: restored to before '%s'", snapshot.Command)
	if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogMessage); err != nil {
		return fmt.Errorf("failed to restore snapshot atomically: %w", err)
	}

	// Delete metadata refs that were created after the snapshot (separate operation)
	currentMetadataRefs, err := e.git.ListMetadata()
	if err == nil {
		var toDelete []string
		for branchName := range currentMetadataRefs {
			if _, exists := snapshot.MetadataSHAs[branchName]; !exists {
				toDelete = append(toDelete, fmt.Sprintf("refs/stackit/metadata/%s", branchName))
			}
		}
		if len(toDelete) > 0 {
			_ = e.git.DeleteRefsBatch(ctx, toDelete)
		}
	}

	// Rebuild engine state
	if err := e.rebuildInternal(true); err != nil {
		return fmt.Errorf("failed to rebuild engine after restore: %w", err)
	}

	// Restore HEAD to the original branch
	if snapshot.CurrentBranch != "" {
		// Check if the branch still exists
		branchExists := false
		for _, branchName := range e.branches {
			if branchName == snapshot.CurrentBranch {
				branchExists = true
				break
			}
		}

		if branchExists {
			branch := e.GetBranch(snapshot.CurrentBranch)
			// If we are already on this branch, checkout might not update the working directory
			// after we've updated the ref. Use reset --hard to be sure.
			current, _ := e.git.GetCurrentBranch()
			if current == branch.GetName() {
				if err := e.git.HardReset(ctx, "HEAD"); err != nil {
					return fmt.Errorf("failed to reset working directory: %w", err)
				}
			} else {
				if err := e.git.CheckoutBranch(ctx, branch.GetName()); err != nil {
					// If checkout fails, try to continue - we're still in a valid state
					_ = err
				} else {
					e.currentBranch = snapshot.CurrentBranch
				}
			}
		} else {
			// Branch was deleted, switch to trunk
			// Access trunk directly while holding the lock (avoid deadlock from e.Trunk() trying to acquire RLock)
			trunkBranch := NewBranch(e.trunk, e)
			if err := e.git.CheckoutBranch(ctx, trunkBranch.GetName()); err != nil {
				return fmt.Errorf("failed to checkout trunk after restore: %w", err)
			}
			e.currentBranch = e.trunk
		}
	}

	return nil
}
