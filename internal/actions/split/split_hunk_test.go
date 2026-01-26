package split

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateDefaultBranchName(t *testing.T) {
	tests := []struct {
		name          string
		originalName  string
		existingNames []string
		want          string
	}{
		{
			name:          "no existing names",
			originalName:  "feature",
			existingNames: []string{},
			want:          "feature_split",
		},
		{
			name:          "simple suffix already taken",
			originalName:  "feature",
			existingNames: []string{"feature_split"},
			want:          "feature_split_2",
		},
		{
			name:          "multiple suffixes taken",
			originalName:  "feature",
			existingNames: []string{"feature_split", "feature_split_2", "feature_split_3"},
			want:          "feature_split_4",
		},
		{
			name:          "non-sequential suffixes taken",
			originalName:  "feature",
			existingNames: []string{"feature_split", "feature_split_3"},
			want:          "feature_split_2",
		},
		{
			name:          "original name in existing doesn't affect result",
			originalName:  "feature",
			existingNames: []string{"feature"},
			want:          "feature_split",
		},
		{
			name:          "empty original name",
			originalName:  "",
			existingNames: []string{},
			want:          "_split",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateDefaultBranchName(tt.originalName, tt.existingNames)
			if got != tt.want {
				t.Errorf("generateDefaultBranchName(%q, %v) = %q, want %q",
					tt.originalName, tt.existingNames, got, tt.want)
			}
		})
	}
}

func TestReadPatchFile(t *testing.T) {
	t.Parallel()

	t.Run("reads file from path", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		patchFile := filepath.Join(tmpDir, "test.patch")

		content := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n"
		require.NoError(t, os.WriteFile(patchFile, []byte(content), 0644))

		result, err := readPatchFile(patchFile)
		require.NoError(t, err)
		require.Equal(t, content, result)
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		t.Parallel()
		_, err := readPatchFile("/nonexistent/path/file.patch")
		require.Error(t, err)
	})

	t.Run("reads empty file", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		patchFile := filepath.Join(tmpDir, "empty.patch")

		require.NoError(t, os.WriteFile(patchFile, []byte{}, 0644))

		result, err := readPatchFile(patchFile)
		require.NoError(t, err)
		require.Equal(t, "", result)
	})

	t.Run("stdin dash with terminal returns error", func(t *testing.T) {
		t.Parallel()
		// When stdin is a terminal (character device), we should get an error
		// In test environments, stdin may or may not be a terminal depending on
		// how the tests are run. We check the actual behavior.
		fi, err := os.Stdin.Stat()
		require.NoError(t, err)

		if (fi.Mode() & os.ModeCharDevice) != 0 {
			// Stdin IS a terminal, so we expect the error
			_, err := readPatchFile("-")
			require.Error(t, err)
			require.Contains(t, err.Error(), "stdin is a terminal")
		} else {
			// Stdin is NOT a terminal (e.g., piped in CI), so it will try to read
			// This test just documents the behavior - in non-terminal mode, it reads stdin
			t.Skip("stdin is not a terminal in this environment, skipping terminal detection test")
		}
	})
}
