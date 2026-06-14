// Package golden provides shared helpers for golden-file (snapshot) tests.
//
// A golden test captures rendered output as a checked-in .golden file so the
// output is reviewable as a diff in PRs and regenerated on demand with -update.
// This package holds the generic machinery (the -update flag, ANSI stripping,
// file compare with a readable diff) so any command can golden its output by
// supplying only its own scenarios. See internal/cli/stack/sync_golden_test.go
// for a worked example.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Update reports whether golden files should be regenerated. Enable it with
// `go test ./... -update`. Go compiles each package's tests into a separate
// binary, so this flag is registered independently per test binary and does
// not collide with -update flags defined in other packages.
var Update = flag.Bool("update", false, "update golden files")

// ansiRegex matches ANSI color escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes ANSI color escape sequences so golden files stay readable
// and stable regardless of terminal color settings.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// Assert compares actual against the golden file at path. With -update it
// (re)writes the file (creating parent directories) instead of comparing. On a
// mismatch it fails the test with a line-by-line diff; on a missing file it
// hints to run with -update.
func Assert(t *testing.T, path, actual string) {
	t.Helper()

	if *Update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("golden: create dir for %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			t.Fatalf("golden: update %s: %v", path, err)
		}
		t.Logf("golden: updated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden: %s does not exist; run with -update to create it.\n\nActual output:\n%s", path, actual)
		}
		t.Fatalf("golden: read %s: %v", path, err)
	}

	if actual != string(want) {
		t.Errorf("golden mismatch for %s (run with -update to accept)\n\nGot:\n%s\n\nWant:\n%s\n\nDiff:\n%s",
			path, actual, string(want), lineDiff(string(want), actual))
	}
}

// lineDiff renders a simple line-by-line diff for debugging mismatches.
func lineDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	var b strings.Builder
	for i := range max(len(wantLines), len(gotLines)) {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			b.WriteString("- ")
			b.WriteString(w)
			b.WriteString("\n+ ")
			b.WriteString(g)
			b.WriteString("\n")
		}
	}
	return b.String()
}
