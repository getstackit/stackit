package pr

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// Trailer key constants for stack metadata embedded in merge commits.
const (
	TrailerStackSize = "Stackit-Stack-Size"
	TrailerPRs       = "Stackit-PRs"
	TrailerScope     = "Stackit-Scope"
)

// StackMetadata contains stack metadata embedded in git trailers.
type StackMetadata struct {
	StackSize int
	PRNumbers []git.PRNumber
	Scope     string
}

// NewStackMetadata constructs stack metadata from explicit fields.
func NewStackMetadata(stackSize int, prNumbers []git.PRNumber, scope string) StackMetadata {
	return StackMetadata{
		StackSize: stackSize,
		PRNumbers: slices.Clone(prNumbers),
		Scope:     scope,
	}
}

// BuildStackMetadata derives stack metadata from merge branches.
func BuildStackMetadata(branches []MergeBranch, scope string) StackMetadata {
	prNumbers := make([]git.PRNumber, 0, len(branches))
	for _, branch := range branches {
		if branch.PRNumber > 0 {
			prNumbers = append(prNumbers, branch.PRNumber)
		}
	}

	return NewStackMetadata(len(branches), prNumbers, scope)
}

// ToTrailers builds a git trailer block for this metadata.
// The trailers follow the standard git trailer format (key: value) and can be
// parsed by git log's %(trailers) format specifier.
func (m StackMetadata) ToTrailers() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s: %d\n", TrailerStackSize, m.StackSize)

	if len(m.PRNumbers) > 0 {
		prStrs := make([]string, len(m.PRNumbers))
		for i, n := range m.PRNumbers {
			prStrs[i] = strconv.Itoa(int(n))
		}
		fmt.Fprintf(&b, "%s: %s\n", TrailerPRs, strings.Join(prStrs, ","))
	}

	if m.Scope != "" {
		fmt.Fprintf(&b, "%s: %s\n", TrailerScope, m.Scope)
	}

	return b.String()
}
