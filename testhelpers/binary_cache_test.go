package testhelpers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheBinaryReusesContentsAcrossConcurrentPackages(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "built")
	require.NoError(t, os.WriteFile(source, []byte("same executable"), 0o755))
	cache := filepath.Join(root, "cache")
	var wg sync.WaitGroup
	paths := make(chan string, 16)
	failures := make(chan error, 16)
	for range 16 {
		wg.Go(func() { path, err := cacheBinary(source, cache); paths <- path; failures <- err })
	}
	wg.Wait()
	close(paths)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	first := ""
	for path := range paths {
		if first == "" {
			first = path
		}
		require.Equal(t, first, path)
	}
	data, err := os.ReadFile(first)
	require.NoError(t, err)
	require.Equal(t, "same executable", string(data))
	entries, err := os.ReadDir(filepath.Dir(first))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary publication files remain")
}

func TestBuildBinaryFallbackCleansTemporaryBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake go command uses a shell script")
	}
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	require.NoError(t, os.Mkdir(tools, 0o755))
	temporary := filepath.Join(root, "tmp")
	require.NoError(t, os.Mkdir(temporary, 0o700))
	cache := filepath.Join(root, "go-cache")
	fake := "#!/bin/sh\nif [ \"$1\" = env ]; then printf '%s\\n' \"$GOCACHE\"; else printf 'same executable' > \"$3\"; fi\n"
	require.NoError(t, os.WriteFile(filepath.Join(tools, "go"), []byte(fake), 0o755))
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", temporary)
	t.Setenv("GOCACHE", cache)
	t.Setenv("STACKIT_TEST_BINARY", "")
	first, err := buildBinary()
	require.NoError(t, err)
	second, err := buildBinary()
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.True(t, strings.HasPrefix(first, cache+string(os.PathSeparator)))
	entries, err := os.ReadDir(temporary)
	require.NoError(t, err)
	require.Empty(t, entries, "fallback must not leak stackit-test-binary directories")
}
