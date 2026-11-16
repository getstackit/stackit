package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffGitHeaderPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   string
		want   string
		wantOK bool
	}{
		{name: "simple path", line: "diff --git a/file.go b/file.go", want: "file.go", wantOK: true},
		{name: "nested path", line: "diff --git a/dir/sub/file.go b/dir/sub/file.go", want: "dir/sub/file.go", wantOK: true},
		{name: "path with spaces", line: "diff --git a/my dir/my file.go b/my dir/my file.go", want: "my dir/my file.go", wantOK: true},
		{name: "path containing b/ segment", line: "diff --git a/x b/y.go b/x b/y.go", want: "x b/y.go", wantOK: true},
		{name: "rename takes new path", line: "diff --git a/old.go b/new.go", want: "new.go", wantOK: true},
		{name: "rename with spaces", line: "diff --git a/old name.go b/new name.go", want: "new name.go", wantOK: true},
		{name: "quoted path is not parsed", line: `diff --git "a/caf\303\251" "b/caf\303\251"`, wantOK: false},
		{name: "not a diff header", line: "--- a/file.go", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := diffGitHeaderPath(tt.line)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseDiffOutput_PathWithSpaces(t *testing.T) {
	t.Parallel()

	diff := `diff --git a/first.go b/first.go
index 1111111..2222222 100644
--- a/first.go
+++ b/first.go
@@ -1 +1 @@
-a
+b
diff --git a/my dir/second file.go b/my dir/second file.go
index 3333333..4444444 100644
--- a/my dir/second file.go
+++ b/my dir/second file.go
@@ -1 +1 @@
-c
+d`

	hunks, err := ParseDiffOutput(diff)
	require.NoError(t, err)
	require.Len(t, hunks, 2)
	require.Equal(t, "first.go", hunks[0].File)
	require.Equal(t, "my dir/second file.go", hunks[1].File)
}
