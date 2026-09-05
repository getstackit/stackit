package testhelpers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBinaryUsesCallerOwnedBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stackit")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("STACKIT_TEST_BINARY", path)

	got, cleanup, err := buildBinaryOnce()
	require.NoError(t, err)
	require.Equal(t, path, got)
	cleanup()
	require.FileExists(t, path, "test packages must not remove the suite's binary")

	got, err = buildBinary()
	require.NoError(t, err)
	require.Equal(t, path, got, "lazy callers must use the same binary")
}

func TestBuildBinaryRejectsInvalidOverride(t *testing.T) {
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "not-executable")
	require.NoError(t, os.WriteFile(notExecutable, nil, 0o600))

	for _, tc := range []struct {
		name string
		path string
	}{
		{"relative path", "relative/stackit"},
		{"missing file", filepath.Join(dir, "missing")},
		{"directory", dir},
		{"not executable", notExecutable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STACKIT_TEST_BINARY", tc.path)
			path, cleanup, err := buildBinaryOnce()
			require.ErrorContains(t, err, "STACKIT_TEST_BINARY")
			require.Empty(t, path)
			require.Nil(t, cleanup)
		})
	}
}
