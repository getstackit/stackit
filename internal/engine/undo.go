// Package engine provides undo/redo functionality through state snapshots
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

const (
	// DefaultMaxUndoStackDepth is the default number of snapshots we keep
	DefaultMaxUndoStackDepth = 10
	// UndoDir is the path, relative to the git directory, where undo snapshots
	// are stored. It is joined onto git.GetGitDir rather than onto the repo
	// root: in a linked worktree `.git` is a file, so joining onto the root
	// gives a path under a file and every snapshot write fails with ENOTDIR.
	// Resolving the git directory also gives each worktree its own undo stack,
	// which is what you want — a snapshot's working-tree capture belongs to the
	// tree it was taken from.
	UndoDir = "stackit/undo"
	// UndoRefPrefix anchors the working-tree captures taken with each snapshot.
	// `git stash create` produces an unreachable commit, which `git gc` is free
	// to collect; a ref keeps the user's uncommitted work alive until the
	// snapshot it belongs to is pruned.
	UndoRefPrefix = "refs/stackit/undo/"
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
	// WorktreeSHA is a stash commit holding the uncommitted changes the
	// command was about to consume. Empty when the working tree was clean, or
	// when it could not be captured.
	WorktreeSHA string `json:"worktree_sha,omitempty"`
	// UntrackedSHA is a commit holding the untracked files present alongside
	// it. Stashes exclude untracked files, but a command that stages
	// everything commits them, so a rollback deletes their only copy.
	UntrackedSHA string `json:"untracked_sha,omitempty"`
}

// SnapshotInfo provides metadata about a snapshot for display
type SnapshotInfo struct {
	ID          string    // Filename without extension
	Command     string    // Command name
	Args        []string  // Command arguments
	Timestamp   time.Time // When the snapshot was taken
	HeadSHA     string    // SHA of the current branch at snapshot time
	DisplayName string    // Human-readable description
}

// SnapshotOptions contains options for taking a snapshot
type SnapshotOptions struct {
	Command string
	Args    []string
	// CaptureWorktree records the uncommitted changes alongside the branch
	// SHAs. Set it for commands that turn the working tree into a commit
	// (modify, create, absorb, split) — restoring their snapshot without the
	// working tree destroys work the user never committed themselves.
	//
	// Left unset, a snapshot costs exactly what it always did. Reconcilers
	// (restack, sync) hold back a worktree with uncommitted changes rather
	// than rebasing it, so they have nothing to consume and nothing to
	// capture; paying `git stash create` on every one of them would be ~35ms
	// of pure overhead per invocation on a 30k-file repository.
	CaptureWorktree bool
}

// getUndoDir returns the path to the undo directory
func getUndoDir(repoRoot string) string {
	return filepath.Join(git.GetGitDir(repoRoot), UndoDir)
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

// snapshotWorktreeRef names the ref anchoring a snapshot's working-tree capture.
func snapshotWorktreeRef(snapshotID string) string {
	return UndoRefPrefix + snapshotID
}

// snapshotUntrackedRef names the ref anchoring a snapshot's untracked-file
// capture. It is a sibling of snapshotWorktreeRef rather than a child, which
// Git would reject as a directory/file conflict.
func snapshotUntrackedRef(snapshotID string) string {
	return UndoRefPrefix + snapshotID + "-untracked"
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

	timestamp, err := time.ParseInLocation("20060102150405.000", timestampStr, time.Local)
	if err != nil {
		// Try without milliseconds for backward compatibility
		var err2 error
		timestamp, err2 = time.ParseInLocation("20060102150405", timestampStr, time.Local)
		if err2 != nil {
			return time.Time{}, "", fmt.Errorf("failed to parse timestamp: %w", err)
		}
	}

	return timestamp, command, nil
}

// TakeSnapshot captures the current state of the repository, including the
// uncommitted changes the command is about to consume.
func (e *engineImpl) TakeSnapshot(ctx context.Context, opts SnapshotOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Ensure undo directory exists
	if err := ensureUndoDir(e.repoRoot); err != nil {
		return fmt.Errorf("failed to create undo directory: %w", err)
	}

	// Get current branch
	currentBranch := e.currentBranch

	// Get all branch SHAs in one git rev-parse call; misses (deleted branches) are omitted.
	branchSHAs, _ := e.git.BatchGetRevisions(e.state.branches)
	if branchSHAs == nil {
		branchSHAs = make(map[string]string)
	}

	// Get all metadata ref SHAs
	metadataRefs, err := e.git.ListMetadata()
	if err != nil {
		// If we can't get metadata refs, continue with empty map
		metadataRefs = make(map[string]string)
	}

	// Convert metadata refs to branch name -> SHA mapping
	metadataSHAs := make(map[string]string)
	maps.Copy(metadataSHAs, metadataRefs)

	// Create snapshot
	timestamp := time.Now()
	filename := getSnapshotFilename(timestamp, opts.Command)
	snapshotID := strings.TrimSuffix(filename, jsonExt)

	var worktreeSHA, untrackedSHA string
	if opts.CaptureWorktree {
		worktreeSHA = e.captureWorktree(ctx, snapshotID, opts.Command)
		untrackedSHA = e.captureUntracked(ctx, snapshotID, opts.Command)
	}

	snapshot := &Snapshot{
		Timestamp:     timestamp,
		Command:       opts.Command,
		Args:          opts.Args,
		CurrentBranch: currentBranch,
		BranchSHAs:    branchSHAs,
		MetadataSHAs:  metadataSHAs,
		WorktreeSHA:   worktreeSHA,
		UntrackedSHA:  untrackedSHA,
	}

	// Serialize to JSON
	jsonData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to file
	filePath := filepath.Join(getUndoDir(e.repoRoot), filename)
	if err := os.WriteFile(filePath, jsonData, 0600); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	// Remember what this run recorded. A conflict workflow binds its rollback
	// to this exact snapshot, so that an abort can never restore an unrelated
	// command's snapshot — see EnterConflictWorkflow.
	e.lastSnapshotID = snapshotID

	// Enforce max stack depth by removing oldest snapshots. Best-effort: the
	// snapshot above was already saved, so a failure here just means the
	// undo stack temporarily exceeds the configured max depth.
	_ = e.enforceMaxStackDepth(ctx) //nolint:errcheck // best-effort

	return nil
}

// captureWorktree records the uncommitted changes present when the snapshot is
// taken, so restoring the snapshot can hand them back.
//
// Rolling branch refs back is only half of "put me back where I started":
// commands like modify and create turn the working tree into a commit, and a
// ref-only rollback leaves that work reachable through nothing but the reflog.
//
// Best effort throughout — a repository that cannot be stashed (mid-rebase, for
// instance) still gets a ref-only snapshot, which is what every snapshot was
// before this existed.
func (e *engineImpl) captureWorktree(ctx context.Context, snapshotID, command string) string {
	sha, err := e.git.StashCreate(ctx, fmt.Sprintf("stackit snapshot: %s", command))
	if err != nil || sha == "" {
		return ""
	}
	// The commit is unreachable until it is anchored. An un-anchored capture is
	// still worth recording — gc is unlikely to run in the seconds before an
	// abort — so a failed anchor does not discard it.
	_ = e.git.UpdateRef(snapshotWorktreeRef(snapshotID), sha)
	return sha
}

// captureUntracked records the untracked files a stash cannot hold. Best effort,
// for the same reasons as captureWorktree.
func (e *engineImpl) captureUntracked(ctx context.Context, snapshotID, command string) string {
	sha, err := e.git.CaptureUntracked(ctx, fmt.Sprintf("stackit snapshot (untracked): %s", command))
	if err != nil || sha == "" {
		return ""
	}
	_ = e.git.UpdateRef(snapshotUntrackedRef(snapshotID), sha)
	return sha
}

// RestoreWorktree re-applies the uncommitted changes captured with a snapshot,
// reporting whether there was anything to restore. A failure names the ref the
// capture is anchored under, so the work stays recoverable by hand.
//
// Callers restore the snapshot's refs first: the capture was taken against
// those commits, so applying it to the rolled-back tree is a clean apply.
func (e *engineImpl) RestoreWorktree(ctx context.Context, snapshotID string) (bool, error) {
	snapshot, err := e.LoadSnapshot(snapshotID)
	if err != nil {
		return false, fmt.Errorf("failed to load snapshot: %w", err)
	}
	if snapshot.WorktreeSHA == "" && snapshot.UntrackedSHA == "" {
		return false, nil
	}

	// Tracked content first, while the rolled-back tree still matches the
	// commits the stash was created against. Untracked files are additive and
	// never collide with it.
	if snapshot.WorktreeSHA != "" {
		if err := e.restoreStash(ctx, snapshot.WorktreeSHA); err != nil {
			return false, fmt.Errorf("%w (recover them with: git stash apply %s)", err, snapshotWorktreeRef(snapshotID))
		}
	}

	if snapshot.UntrackedSHA != "" {
		if _, err := e.git.RestoreUntracked(ctx, snapshot.UntrackedSHA); err != nil {
			return false, fmt.Errorf("%w (recover them with: git restore --source %s --worktree -- .)", err, snapshotUntrackedRef(snapshotID))
		}
	}

	return true, nil
}

// restoreStash re-applies a captured stash, preferring the staged/unstaged
// split it was created with.
func (e *engineImpl) restoreStash(ctx context.Context, stashSHA string) error {
	indexErr := e.git.StashApplyRef(ctx, stashSHA, git.StashApplyWithIndex)
	if indexErr == nil {
		return nil
	}

	// --index writes the working tree first and reinstates the staged/unstaged
	// split afterwards, so a failure can leave the changes already applied and
	// only the split missing. Retrying then applies the same diff onto itself
	// and litters the tree with conflict markers. Callers arrive here with a
	// clean tree — refs restored, working tree reset — so a tree that is still
	// clean is proof the failed attempt wrote nothing and a retry is safe.
	dirty, dirtyErr := e.git.WorktreeHasTrackedChanges(ctx, e.repoRoot)
	if dirtyErr != nil || dirty {
		return fmt.Errorf("failed to restore uncommitted changes: %w", indexErr)
	}

	// Nothing landed. Losing which hunks were staged beats losing the changes.
	if err := e.git.StashApplyRef(ctx, stashSHA, git.StashApplyWorktreeOnly); err != nil {
		return fmt.Errorf("failed to restore uncommitted changes: %w", err)
	}
	return nil
}

// LastSnapshotID returns the snapshot this engine recorded, or empty when it
// has taken none. It answers "did the command now running record a rollback
// point", which is what binds a conflict workflow to its own snapshot.
//
// That reading holds because the CLI runs one command per process, so an
// engine that has taken no snapshot reports empty. A long-lived engine — the
// API server keeps one per repository across requests — carries the value
// forward, so a later command that takes no snapshot of its own would inherit
// the previous one. Any command that can enter the conflict workflow must
// therefore take a snapshot before it can conflict; every one of them does.
// Add a new conflict-capable path without one and it will silently bind to a
// stranger's rollback point on the server.
func (e *engineImpl) LastSnapshotID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSnapshotID
}

// enforceMaxStackDepth removes the oldest snapshots if we exceed MaxUndoStackDepth
func (e *engineImpl) enforceMaxStackDepth(ctx context.Context) error {
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
	var captureRefs []string
	for i := range toDelete {
		// Read the snapshot before removing it: it names the captures that
		// have to go with it. Snapshots that captured nothing — every command
		// that does not consume the working tree — contribute no refs, so
		// pruning them stays free of git processes.
		snapshotID := strings.TrimSuffix(snapshots[i].Name(), jsonExt)
		var owned []string
		if snapshot, err := e.LoadSnapshot(snapshotID); err == nil {
			if snapshot.WorktreeSHA != "" {
				owned = append(owned, snapshotWorktreeRef(snapshotID))
			}
			if snapshot.UntrackedSHA != "" {
				owned = append(owned, snapshotUntrackedRef(snapshotID))
			}
		}

		filePath := filepath.Join(dir, snapshots[i].Name())
		if err := os.Remove(filePath); err != nil {
			// Continue deleting others even if one fails. The captures stay
			// anchored: a snapshot still on disk names them, and dropping its
			// refs would leave it pointing at commits gc is free to collect.
			continue
		}
		captureRefs = append(captureRefs, owned...)
	}

	// The captures are only reachable through these refs, so they go when the
	// snapshots that own them go. One batched write, not one per ref.
	if len(captureRefs) > 0 {
		_ = e.git.DeleteRefsBatch(ctx, captureRefs)
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

		// Get HEAD SHA from snapshot (current branch's SHA at snapshot time)
		headSHA := snapshot.BranchSHAs[snapshot.CurrentBranch]

		// Generate display name
		displayName := formatSnapshotDisplay(command, snapshot.Args, timestamp, headSHA)

		snapshots = append(snapshots, SnapshotInfo{
			ID:          entry.Name()[:len(entry.Name())-len(jsonExt)], // Remove .json
			Command:     command,
			Args:        snapshot.Args,
			Timestamp:   timestamp,
			HeadSHA:     headSHA,
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
func formatSnapshotDisplay(command string, args []string, timestamp time.Time, headSHA string) string {
	// Truncate SHA to 12 chars
	shortSHA := headSHA
	if len(shortSHA) > 12 {
		shortSHA = shortSHA[:12]
	}

	// Format timestamp in local time
	timeStr := timestamp.Local().Format("2006-01-02 15:04:05")

	// Format command tag in uppercase brackets
	tag := strings.ToUpper(command)

	// Build description from args
	description := command
	if len(args) > 0 {
		displayArgs := args
		if len(displayArgs) > 2 {
			displayArgs = displayArgs[:2]
		}
		description = fmt.Sprintf("%s %s", command, strings.Join(displayArgs, " "))
	}

	return fmt.Sprintf("%s %s [%s] %s", shortSHA, timeStr, tag, description)
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
	currentBranches, err := e.git.GetAllBranchNames(ctx)
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
		branchExists := slices.Contains(e.state.branches, snapshot.CurrentBranch)

		if branchExists {
			branch := e.GetBranch(snapshot.CurrentBranch)
			// If we are already on this branch, checkout might not update the working directory
			// after we've updated the ref. Use reset --hard to be sure.
			current, _ := e.git.GetCurrentBranch()
			if current == branch.GetName() {
				if err := e.git.HardReset(ctx, "HEAD"); err != nil {
					return fmt.Errorf("failed to reset working directory: %w", err)
				}
			} else if err := e.git.CheckoutBranch(ctx, branch.GetName()); err == nil {
				e.currentBranch = snapshot.CurrentBranch
			}
			// If checkout fails, continue anyway - the restore already applied the refs,
			// so we're still in a valid state even if HEAD wasn't switched.
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
