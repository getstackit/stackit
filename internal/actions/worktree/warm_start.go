package worktree

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"

	"github.com/getstackit/stackit/internal/engine"
)

// WorktreeIncludeFile is the repository-root file that selects ignored files
// which may be copied into a newly-created worktree. Its patterns use
// .gitignore syntax, but a match alone is insufficient: callers must pass
// only files already known to be ignored by Git.
const WorktreeIncludeFile = ".worktreeinclude"

// WarmStartResult describes files copied into a new worktree. Paths are
// relative to the source worktree and sorted for deterministic output.
type WarmStartResult struct {
	Enabled         bool
	Copied          []string
	SkippedExisting []string
	// SkippedUnsafe lists files that could not be placed because something on
	// their path is not a plain directory — most often a tracked file on trunk
	// occupying a name the include file also selects as a directory.
	SkippedUnsafe []WarmStartSkip
}

// WarmStartSkip records one file warm start declined to copy, and why.
type WarmStartSkip struct {
	Path   string
	Reason string
}

// errWarmStartSkip marks a per-file problem. Warm start is an optimization:
// one unplaceable file must not fail worktree creation, which is what an error
// return does — the caller tears the whole worktree down.
var errWarmStartSkip = errors.New("warm-start path cannot be used")

func warmStartWorktree(ctx context.Context, eng engine.Engine, sourceRoot, destinationRoot string) (WarmStartResult, error) {
	if _, err := os.Stat(filepath.Join(sourceRoot, WorktreeIncludeFile)); os.IsNotExist(err) {
		return WarmStartResult{}, nil
	} else if err != nil {
		return WarmStartResult{}, fmt.Errorf("inspect %s: %w", WorktreeIncludeFile, err)
	}

	ignoredPaths, err := eng.ListIgnoredFiles(ctx, sourceRoot)
	if err != nil {
		return WarmStartResult{}, err
	}
	return CopyIncludedIgnoredFiles(sourceRoot, destinationRoot, ignoredPaths)
}

// CopyIncludedIgnoredFiles copies selected ignored regular files from sourceRoot
// to destinationRoot. ignoredPaths must be repository-relative paths that Git
// has already classified as ignored; this function deliberately does not infer
// ignore state from .worktreeinclude itself.
//
// Existing destination files are never overwritten. Symlinks and non-regular
// files are skipped so a project-controlled include file cannot cause Stackit
// to follow a path outside either worktree.
func CopyIncludedIgnoredFiles(sourceRoot, destinationRoot string, ignoredPaths []string) (WarmStartResult, error) {
	includePath := filepath.Join(sourceRoot, WorktreeIncludeFile)
	matcher, err := ignore.CompileIgnoreFile(includePath)
	if os.IsNotExist(err) {
		return WarmStartResult{}, nil
	}
	if err != nil {
		return WarmStartResult{}, fmt.Errorf("read %s: %w", WorktreeIncludeFile, err)
	}

	result := WarmStartResult{Enabled: true}
	// Verified directories are remembered per root. Files selected for warm
	// start share long prefixes — node_modules/... being the motivating case —
	// and re-walking every component of every path costs one Lstat per
	// component per file.
	sourceVerified := map[string]bool{}
	destinationVerified := map[string]bool{}
	for _, path := range uniqueSortedPaths(ignoredPaths) {
		relativePath, err := warmStartRelativePath(path)
		if err != nil {
			return WarmStartResult{}, err
		}
		if !matcher.MatchesPath(filepath.ToSlash(relativePath)) {
			continue
		}

		sourcePath := filepath.Join(sourceRoot, relativePath)
		if err := ensureWarmStartParents(sourceRoot, relativePath, false, sourceVerified); err != nil {
			if skip, ok := asWarmStartSkip(relativePath, err); ok {
				result.SkippedUnsafe = append(result.SkippedUnsafe, skip)
				continue
			}
			return WarmStartResult{}, err
		}
		info, err := os.Lstat(sourcePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return WarmStartResult{}, fmt.Errorf("inspect warm-start source %s: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}

		// Validate the destination's parents before stat-ing the destination
		// itself: if a component is a plain file, that stat fails with a
		// platform-specific "not a directory" errno rather than "does not
		// exist", and the path check reports the same condition portably.
		destinationPath := filepath.Join(destinationRoot, relativePath)
		if err := ensureWarmStartParents(destinationRoot, relativePath, true, destinationVerified); err != nil {
			if skip, ok := asWarmStartSkip(relativePath, err); ok {
				result.SkippedUnsafe = append(result.SkippedUnsafe, skip)
				continue
			}
			return WarmStartResult{}, err
		}
		if _, err := os.Lstat(destinationPath); err == nil {
			result.SkippedExisting = append(result.SkippedExisting, relativePath)
			continue
		} else if !os.IsNotExist(err) {
			return WarmStartResult{}, fmt.Errorf("inspect warm-start destination %s: %w", relativePath, err)
		}

		if err := copyWarmStartFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
			return WarmStartResult{}, fmt.Errorf("copy warm-start file %s: %w", relativePath, err)
		}
		result.Copied = append(result.Copied, relativePath)
	}

	return result, nil
}

func uniqueSortedPaths(paths []string) []string {
	paths = slices.Clone(paths)
	slices.Sort(paths)
	return slices.Compact(paths)
}

func warmStartRelativePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("warm-start path must be relative: %s", path)
	}

	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("invalid warm-start path: %s", path)
	}
	if strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("warm-start path escapes repository: %s", path)
	}

	return cleaned, nil
}

func copyWarmStartFile(sourcePath, destinationPath string, mode os.FileMode) (err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := destination.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(destinationPath)
		}
	}()

	_, err = io.Copy(destination, source)
	return err
}

// asWarmStartSkip converts a per-file refusal into a recorded skip. Anything
// else is a real failure and stays an error.
func asWarmStartSkip(relativePath string, err error) (WarmStartSkip, bool) {
	if !errors.Is(err, errWarmStartSkip) {
		return WarmStartSkip{}, false
	}
	return WarmStartSkip{Path: relativePath, Reason: err.Error()}, true
}

// ensureWarmStartParents verifies (and optionally creates) every directory
// leading to relativePath under root.
//
// verified memoizes directories already checked under this root for this run.
// Warm-start sets share deep prefixes, so without it each file re-Lstats every
// component of its path on both roots.
func ensureWarmStartParents(root, relativePath string, create bool, verified map[string]bool) error {
	directory := filepath.Dir(relativePath)
	if directory == "." {
		return nil
	}

	current := root
	for _, component := range strings.Split(directory, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if verified[current] {
			continue
		}
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && create {
			if mkErr := os.Mkdir(current, 0o750); mkErr != nil && !os.IsExist(mkErr) {
				// Most often a tracked file already occupies this name, which
				// is a conflict with this one file's path — not a reason to
				// abandon the worktree.
				return fmt.Errorf("%w: cannot create directory %s: %w", errWarmStartSkip, current, mkErr)
			}
			info, err = os.Lstat(current)
		}
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect warm-start directory %s: %w", current, err)
		}
		// A symlink stays a hard failure. The include file is project-controlled,
		// so a symlinked parent is how a warm start would be steered into
		// writing outside the worktree — that is worth refusing loudly, not
		// noting and moving on.
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("warm-start path has unsafe parent directory: %s", current)
		}
		// A plain file occupying a directory name is mundane by comparison: a
		// tracked file on trunk whose name the include file also selects as a
		// directory. Only this one file is unplaceable.
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is a file, not a directory", errWarmStartSkip, current)
		}
		verified[current] = true
	}

	return nil
}
