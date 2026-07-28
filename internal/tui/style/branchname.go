package style

import (
	"strings"
	"unicode"
)

// DisplayBranchName returns a compact branch name for command output. Branches
// created by `stackit create` are named `<user>/<timestamp>/<slug>`; the first
// two segments carry no information a reader needs, and they dominate the line
// width when a name appears next to its parent or a PR number. The full name is
// still used anywhere the output is meant to be copy-pasted (e.g. the
// `st restack <branch>` advice line).
func DisplayBranchName(branchName string) string {
	parts := strings.Split(branchName, "/")
	if len(parts) >= 3 && isTimestampSegment(parts[1]) {
		return strings.Join(parts[2:], "/")
	}
	return branchName
}

func isTimestampSegment(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
