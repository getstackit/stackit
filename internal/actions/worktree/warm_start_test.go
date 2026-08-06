package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyIncludedIgnoredFiles(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()

	writeWarmStartFile(t, source, WorktreeIncludeFile, ".env\ncache/\n!cache/keep.txt\n")
	writeWarmStartFile(t, source, ".env", "secret")
	writeWarmStartFile(t, source, "cache/data.txt", "cached")
	writeWarmStartFile(t, source, "cache/keep.txt", "do not copy")
	writeWarmStartFile(t, source, "other.txt", "not selected")

	result, err := CopyIncludedIgnoredFiles(source, destination, []string{
		"other.txt", "cache/keep.txt", ".env", "cache/data.txt", "cache/data.txt",
	})
	require.NoError(t, err)
	require.True(t, result.Enabled)
	require.Equal(t, []string{".env", "cache/data.txt"}, result.Copied)
	require.Empty(t, result.SkippedExisting)

	requireWarmStartFile(t, destination, ".env", "secret")
	requireWarmStartFile(t, destination, "cache/data.txt", "cached")
	require.NoFileExists(t, filepath.Join(destination, "cache", "keep.txt"))
	require.NoFileExists(t, filepath.Join(destination, "other.txt"))
}

func TestCopyIncludedIgnoredFilesDoesNotOverwriteExistingFiles(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, ".env\n")
	writeWarmStartFile(t, source, ".env", "source")
	writeWarmStartFile(t, destination, ".env", "destination")

	result, err := CopyIncludedIgnoredFiles(source, destination, []string{".env"})
	require.NoError(t, err)
	require.True(t, result.Enabled)
	require.Empty(t, result.Copied)
	require.Equal(t, []string{".env"}, result.SkippedExisting)
	requireWarmStartFile(t, destination, ".env", "destination")
}

func TestCopyIncludedIgnoredFilesPreservesExecutableMode(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "bin/tool\n")
	writeWarmStartFile(t, source, "bin/tool", "#!/bin/sh\n")
	require.NoError(t, os.Chmod(filepath.Join(source, "bin", "tool"), 0o755))

	_, err := CopyIncludedIgnoredFiles(source, destination, []string{"bin/tool"})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(destination, "bin", "tool"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

func TestCopyIncludedIgnoredFilesSkipsSymlinks(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "linked\n")
	writeWarmStartFile(t, source, "real.txt", "real")
	require.NoError(t, os.Symlink("real.txt", filepath.Join(source, "linked")))

	result, err := CopyIncludedIgnoredFiles(source, destination, []string{"linked"})
	require.NoError(t, err)
	require.Empty(t, result.Copied)
	require.NoFileExists(t, filepath.Join(destination, "linked"))
}

func TestCopyIncludedIgnoredFilesRefusesSymlinkDestinationParents(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	outside := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "config/secret\n")
	writeWarmStartFile(t, source, "config/secret", "secret")
	require.NoError(t, os.Symlink(outside, filepath.Join(destination, "config")))

	_, err := CopyIncludedIgnoredFiles(source, destination, []string{"config/secret"})
	require.ErrorContains(t, err, "unsafe parent directory")
	require.NoFileExists(t, filepath.Join(outside, "secret"))
}

func TestCopyIncludedIgnoredFilesRejectsEscapingPaths(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "*\n")

	_, err := CopyIncludedIgnoredFiles(source, destination, []string{"../outside"})
	require.ErrorContains(t, err, "escapes repository")
}

func TestCopyIncludedIgnoredFilesWithoutIncludeFileIsDisabled(t *testing.T) {
	result, err := CopyIncludedIgnoredFiles(t.TempDir(), t.TempDir(), []string{".env"})
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Empty(t, result.Copied)
}

func writeWarmStartFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func requireWarmStartFile(t *testing.T, root, relativePath, expected string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, relativePath))
	require.NoError(t, err)
	require.Equal(t, expected, string(contents))
}

// TestCopyIncludedIgnoredFilesSkipsFileOccupyingDirectoryName covers a warm-start
// selection that cannot be placed because a tracked file already owns the name.
//
// Any warm-start error tears down the whole worktree — the caller cleans up on
// failure — so one unplaceable file used to cost the entire `create -w`. Warm
// start is an optimization; it reports the file and continues.
func TestCopyIncludedIgnoredFilesSkipsFileOccupyingDirectoryName(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "config/secret\nkeep.env\n")
	writeWarmStartFile(t, source, "config/secret", "secret")
	writeWarmStartFile(t, source, "keep.env", "kept")

	// Trunk carries a tracked file named "config", so the directory the include
	// file wants cannot exist.
	writeWarmStartFile(t, destination, "config", "tracked file on trunk")

	result, err := CopyIncludedIgnoredFiles(source, destination, []string{"config/secret", "keep.env"})
	require.NoError(t, err, "one unplaceable file must not fail the whole warm start")

	require.Len(t, result.SkippedUnsafe, 1)
	require.Equal(t, "config/secret", result.SkippedUnsafe[0].Path)
	require.Contains(t, result.SkippedUnsafe[0].Reason, "not a directory")

	// Everything else still gets copied.
	require.Equal(t, []string{"keep.env"}, result.Copied)
	require.FileExists(t, filepath.Join(destination, "keep.env"))

	// The tracked file is untouched.
	contents, err := os.ReadFile(filepath.Join(destination, "config"))
	require.NoError(t, err)
	require.Equal(t, "tracked file on trunk", string(contents))
}

// TestCopyIncludedIgnoredFilesVerifiesEachDirectoryOnce guards the memoization
// that keeps deep warm-start sets (node_modules being the motivating case) from
// re-walking every path component for every file.
func TestCopyIncludedIgnoredFilesVerifiesEachDirectoryOnce(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	writeWarmStartFile(t, source, WorktreeIncludeFile, "deps/**\n")

	names := []string{"a", "b", "c", "d"}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		relativePath := filepath.Join("deps", "nested", "deeper", name)
		writeWarmStartFile(t, source, relativePath, name)
		paths = append(paths, filepath.ToSlash(relativePath))
	}

	result, err := CopyIncludedIgnoredFiles(source, destination, paths)
	require.NoError(t, err)
	require.Len(t, result.Copied, 4)
	for _, name := range []string{"a", "b", "c", "d"} {
		require.FileExists(t, filepath.Join(destination, "deps", "nested", "deeper", name))
	}
}
