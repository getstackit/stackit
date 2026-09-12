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
// `st restack --branch <branch>` advice line).
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

// BranchNameResolver shortens branch names for terminal rows without making
// two branches look alike. DisplayBranchName drops the `<user>/<timestamp>/`
// prefix, so `alice/…/fix-tests` and `bob/…/fix-tests` both shorten to
// `fix-tests`; once a run has seen two names that share a short form, both
// render in full.
//
// Seed it with every branch the run may render: a row that already printed
// short cannot be rewritten when its twin shows up later. It also learns names
// as they are rendered, for branches missing from the seed set. Not safe for
// concurrent use.
type BranchNameResolver struct {
	owners    map[string]string // short form → first full name seen with it
	ambiguous map[string]bool   // short forms shared by two or more full names
}

// NewBranchNameResolver returns a resolver that already knows names.
func NewBranchNameResolver(names ...string) *BranchNameResolver {
	r := &BranchNameResolver{owners: map[string]string{}, ambiguous: map[string]bool{}}
	r.Observe(names...)
	return r
}

// Observe records names so later lookups account for their collisions.
func (r *BranchNameResolver) Observe(names ...string) {
	for _, name := range names {
		if name == "" {
			continue
		}
		short := DisplayBranchName(name)
		owner, seen := r.owners[short]
		switch {
		case !seen:
			r.owners[short] = name
		case owner != name:
			r.ambiguous[short] = true
		}
	}
}

// Short returns DisplayBranchName(name), or name itself when its short form is
// shared with another branch this resolver has seen.
func (r *BranchNameResolver) Short(name string) string {
	r.Observe(name)
	short := DisplayBranchName(name)
	if r.ambiguous[short] {
		return name
	}
	return short
}
