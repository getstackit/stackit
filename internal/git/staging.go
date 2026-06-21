package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

func (r *runner) StageAll(ctx context.Context) error {
	if _, err := r.RunGitCommandWithContext(ctx, "add", "-A"); err != nil {
		return fmt.Errorf("failed to stage all changes: %w", err)
	}
	return nil
}

func (r *runner) StagePatch(_ context.Context) error {
	return r.RunGitCommandInteractive("add", "-p")
}

func (r *runner) StageTracked(ctx context.Context) error {
	if _, err := r.RunGitCommandWithContext(ctx, "add", "-u"); err != nil {
		return fmt.Errorf("failed to stage tracked changes: %w", err)
	}
	return nil
}

func (r *runner) AddAll(ctx context.Context) error {
	return r.StageAll(ctx)
}

func (r *runner) StageChanges(ctx context.Context, opts StagingOptions) error {
	if opts.Patch && !opts.All {
		return r.RunGitCommandInteractive("add", "-p")
	}

	if opts.All {
		return r.StageAll(ctx)
	}

	if opts.Update {
		return r.StageTracked(ctx)
	}

	return nil
}

// HasStagedChanges reports whether the index has changes relative to HEAD.
// `git diff --cached --quiet` exits 0 when there are no differences and 1
// when there are; any other non-zero exit is a real failure.
func (r *runner) HasStagedChanges(ctx context.Context) (bool, error) {
	return diffHasChanges(r, ctx, "diff", "--cached", "--quiet")
}

// HasUnstagedChanges reports whether the working tree differs from the index
// (tracked-file modifications only — untracked files are reported via
// HasUntrackedFiles).
func (r *runner) HasUnstagedChanges(ctx context.Context) (bool, error) {
	return diffHasChanges(r, ctx, "diff", "--quiet")
}

func diffHasChanges(r *runner, ctx context.Context, args ...string) (bool, error) {
	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err == nil {
		return false, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("failed to check %v: %w", args, err)
}

func (r *runner) HasUntrackedFiles(ctx context.Context) (bool, error) {
	files, err := r.listUntrackedFiles(ctx)
	if err != nil {
		return false, err
	}
	return len(files) > 0, nil
}

func (r *runner) GetUntrackedFiles(ctx context.Context) ([]string, error) {
	files, err := r.listUntrackedFiles(ctx)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	slices.Sort(files)
	return files, nil
}

// listUntrackedFiles returns all untracked, non-ignored files. Uses
// `git ls-files --others --exclude-standard -z` so paths with newlines or
// shell metacharacters round-trip cleanly.
func (r *runner) listUntrackedFiles(ctx context.Context) ([]string, error) {
	out, err := r.RunGitCommandRawWithContext(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("failed to list untracked files: %w", err)
	}
	return splitNulTerminated(out), nil
}

func (r *runner) ParseStagedHunks(ctx context.Context) ([]Hunk, error) {
	diffOutput, err := r.RunGitCommandRawWithContext(ctx, "diff", "--cached")
	if err != nil {
		return nil, fmt.Errorf("failed to get staged diff: %w", err)
	}

	return ParseDiffOutput(diffOutput)
}

// StageHunks stages specific hunks by applying them as patches to the index.
// This allows selective staging without using interactive git add -p.
// New files are handled specially since git apply --cached cannot create files
// that don't exist in the working tree.
func (r *runner) StageHunks(ctx context.Context, hunks []Hunk) error {
	if len(hunks) == 0 {
		return nil
	}

	// Separate new files from modifications
	var newFileHunks []Hunk
	var modHunks []Hunk
	for _, h := range hunks {
		if h.IsNewFile {
			newFileHunks = append(newFileHunks, h)
		} else {
			modHunks = append(modHunks, h)
		}
	}

	// Handle new files: extract content and write to disk, then stage with git add
	for _, h := range newFileHunks {
		if err := r.stageNewFileHunk(ctx, h); err != nil {
			return err
		}
	}

	// Handle modifications with existing git apply --cached
	if len(modHunks) > 0 {
		patch := BuildPatchFromHunks(modHunks)
		if patch != "" {
			// Apply the patch to the index using git apply --cached
			// We use --3way to handle conflicts better
			_, err := r.runGitInternal(ctx, patch, nil, true, "apply", "--cached", "--3way")
			if err != nil {
				// Try without --3way as a fallback (some git versions have issues with --3way)
				_, fallbackErr := r.runGitInternal(ctx, patch, nil, true, "apply", "--cached")
				if fallbackErr != nil {
					// Check if any files are misdetected new files and handle them
					rescuedHunks, remainingHunks := r.rescueMisdetectedNewFiles(ctx, modHunks)
					if len(rescuedHunks) > 0 {
						// Stage the rescued new files
						for _, h := range rescuedHunks {
							if err := r.stageNewFileHunk(ctx, h); err != nil {
								return fmt.Errorf("failed to stage misdetected new file %s: %w", h.File, err)
							}
						}
						// Try to apply remaining modifications if any
						if len(remainingHunks) > 0 {
							remainingPatch := BuildPatchFromHunks(remainingHunks)
							if remainingPatch != "" {
								_, retryErr := r.runGitInternal(ctx, remainingPatch, nil, true, "apply", "--cached")
								if retryErr != nil {
									return fmt.Errorf("failed to apply patch after rescuing new files: %w", retryErr)
								}
							}
						}
						return nil
					}
					return fmt.Errorf("failed to apply patch: %w (note: --3way also failed)", fallbackErr)
				}
			}
		}
	}

	return nil
}

// stageNewFileHunk handles staging a single new file hunk by writing content to disk and staging it.
func (r *runner) stageNewFileHunk(ctx context.Context, h Hunk) error {
	content := extractContentFromHunk(h)
	filePath := filepath.Join(r.getRepoRoot(), h.File)

	// Create parent directories if they don't exist
	if dir := filepath.Dir(filePath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory for new file %s: %w", h.File, err)
		}
	}

	// Handle symlinks specially (FileMode 120000)
	if h.FileMode == "120000" {
		// For symlinks, the content is the target path
		target := strings.TrimSpace(content)
		if err := os.Symlink(target, filePath); err != nil {
			return fmt.Errorf("failed to create symlink %s: %w", h.File, err)
		}
	} else {
		// Regular file - determine mode (default to 0644)
		fileMode := os.FileMode(0o644)
		if h.FileMode == "100755" {
			fileMode = 0o755
		}

		if err := os.WriteFile(filePath, []byte(content), fileMode); err != nil {
			return fmt.Errorf("failed to write new file %s: %w", h.File, err)
		}
	}

	if _, err := r.RunGitCommandWithContext(ctx, "add", "--", h.File); err != nil {
		return fmt.Errorf("failed to stage new file %s: %w", h.File, err)
	}
	return nil
}

// rescueMisdetectedNewFiles checks if any modification hunks are actually for new files
// that weren't properly detected. Returns hunks that should be treated as new files
// and the remaining modification hunks.
func (r *runner) rescueMisdetectedNewFiles(ctx context.Context, modHunks []Hunk) (newFiles, remaining []Hunk) {
	// Group hunks by file to check each file once
	fileHunks := make(map[string][]Hunk)
	for _, h := range modHunks {
		fileHunks[h.File] = append(fileHunks[h.File], h)
	}

	for file, hunks := range fileHunks {
		// Check if file exists in working tree but not in index
		// This indicates a new file that wasn't properly detected
		existsInIndex, err := r.fileExistsInIndex(ctx, file)
		if err != nil {
			// On error, keep as modification to preserve original behavior
			remaining = append(remaining, hunks...)
			continue
		}

		if !existsInIndex {
			// File doesn't exist in index - treat as new file
			for _, h := range hunks {
				h.IsNewFile = true
				newFiles = append(newFiles, h)
			}
		} else {
			remaining = append(remaining, hunks...)
		}
	}
	return newFiles, remaining
}

// fileExistsInIndex checks if a file exists in the git index. Uses
// `git ls-files --error-unmatch` which exits 1 when the path is not tracked;
// that's the cheapest one-shot index probe. Other non-zero exits (e.g., 128
// for "not a git repo" or argument errors) surface as real errors so callers
// don't silently treat them as "not tracked".
func (r *runner) fileExistsInIndex(ctx context.Context, file string) (bool, error) {
	_, err := r.RunGitCommandWithContext(ctx, "ls-files", "--error-unmatch", "-z", "--", file)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("failed to read index for %s: %w", file, err)
}

// extractContentFromHunk extracts the file content from a new file hunk.
// It parses the unified diff format and returns only the added lines (without the + prefix).
// Respects the "\ No newline at end of file" marker to preserve files without trailing newlines.
func extractContentFromHunk(h Hunk) string {
	var lines []string
	hasNoNewlineMarker := false
	for line := range strings.SplitSeq(h.Content, "\n") {
		// Skip the hunk header line (@@ ... @@)
		if strings.HasPrefix(line, "@@") {
			continue
		}
		// Check for the "no newline at end of file" marker
		if line == "\\ No newline at end of file" {
			hasNoNewlineMarker = true
			continue
		}
		// Include lines that start with + (but not the +++ header)
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines = append(lines, strings.TrimPrefix(line, "+"))
		}
	}
	result := strings.Join(lines, "\n")
	// Add trailing newline only if the original file had one (no marker present)
	if len(lines) > 0 && !hasNoNewlineMarker {
		result += "\n"
	}
	return result
}

// UnstageAll removes all changes from the staging area.
func (r *runner) UnstageAll(ctx context.Context) error {
	if _, err := r.RunGitCommandWithContext(ctx, "reset"); err != nil {
		return fmt.Errorf("failed to unstage changes: %w", err)
	}
	return nil
}
