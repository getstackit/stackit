package git

import "testing"

func TestParseDiffHunks_ExplicitZeroCounts(t *testing.T) {
	t.Parallel()

	t.Run("new file keeps zero old count", func(t *testing.T) {
		t.Parallel()
		diff := `diff --git a/file.go b/file.go
new file mode 100644
index 0000000..abc123
--- /dev/null
+++ b/file.go
@@ -0,0 +1,3 @@
+line1
+line2
+line3`
		hunks := parseDiffHunks(diff, "file.go")
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(hunks))
		}
		if hunks[0].OldCount != 0 {
			t.Errorf("expected OldCount 0 for new file, got %d", hunks[0].OldCount)
		}
		if hunks[0].NewCount != 3 {
			t.Errorf("expected NewCount 3, got %d", hunks[0].NewCount)
		}
	})

	t.Run("deleted file keeps zero new count", func(t *testing.T) {
		t.Parallel()
		diff := `diff --git a/file.go b/file.go
deleted file mode 100644
index abc123..0000000
--- a/file.go
+++ /dev/null
@@ -1,3 +0,0 @@
-line1
-line2
-line3`
		hunks := parseDiffHunks(diff, "file.go")
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(hunks))
		}
		if hunks[0].OldCount != 3 {
			t.Errorf("expected OldCount 3, got %d", hunks[0].OldCount)
		}
		if hunks[0].NewCount != 0 {
			t.Errorf("expected NewCount 0 for deleted file, got %d", hunks[0].NewCount)
		}
	})

	t.Run("omitted count defaults to one", func(t *testing.T) {
		t.Parallel()
		diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1 +1,2 @@
 context
+added`
		hunks := parseDiffHunks(diff, "file.go")
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(hunks))
		}
		if hunks[0].OldCount != 1 {
			t.Errorf("expected OldCount 1 when omitted, got %d", hunks[0].OldCount)
		}
	})
}
