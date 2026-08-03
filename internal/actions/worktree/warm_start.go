package worktree

import (
	"context"
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
}

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
	for _, path := range uniqueSortedPaths(ignoredPaths) {
		relativePath, err := warmStartRelativePath(path)
		if err != nil {
			return WarmStartResult{}, err
		}
		if !matcher.MatchesPath(filepath.ToSlash(relativePath)) {
			continue
		}

		sourcePath := filepath.Join(sourceRoot, relativePath)
		if err := ensureWarmStartParents(sourceRoot, relativePath, false); err != nil {
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

		destinationPath := filepath.Join(destinationRoot, relativePath)
		if _, err := os.Lstat(destinationPath); err == nil {
			result.SkippedExisting = append(result.SkippedExisting, relativePath)
			continue
		} else if !os.IsNotExist(err) {
			return WarmStartResult{}, fmt.Errorf("inspect warm-start destination %s: %w", relativePath, err)
		}
		if err := ensureWarmStartParents(destinationRoot, relativePath, true); err != nil {
			return WarmStartResult{}, err
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

func ensureWarmStartParents(root, relativePath string, create bool) error {
	directory := filepath.Dir(relativePath)
	if directory == "." {
		return nil
	}

	current := root
	for _, component := range strings.Split(directory, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && create {
			if err := os.Mkdir(current, 0o750); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create warm-start directory %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect warm-start directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("warm-start path has unsafe parent directory: %s", current)
		}
	}

	return nil
}
