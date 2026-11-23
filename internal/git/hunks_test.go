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

func TestParseHunkHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   string
		want   hunkHeader
		wantOK bool
	}{
		{
			name:   "explicit counts",
			line:   "@@ -10,5 +12,6 @@",
			want:   hunkHeader{OldStart: 10, OldCount: 5, NewStart: 12, NewCount: 6},
			wantOK: true,
		},
		{
			name:   "omitted counts default to one",
			line:   "@@ -3 +4 @@",
			want:   hunkHeader{OldStart: 3, OldCount: 1, NewStart: 4, NewCount: 1},
			wantOK: true,
		},
		{
			name:   "explicit zero count stays zero",
			line:   "@@ -0,0 +1,3 @@",
			want:   hunkHeader{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 3},
			wantOK: true,
		},
		{
			name:   "section heading suffix is captured",
			line:   "@@ -1,2 +1,3 @@ func Foo()",
			want:   hunkHeader{OldStart: 1, OldCount: 2, NewStart: 1, NewCount: 3, Suffix: " func Foo()"},
			wantOK: true,
		},
		{
			name: "not a hunk header",
			line: "+@@ -1,2 +1,3 @@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseHunkHeader(tt.line)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
