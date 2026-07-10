package pr

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	PRNumbers []int
	Scope     string
}

// NewStackMetadata constructs stack metadata from explicit fields.
func NewStackMetadata(stackSize int, prNumbers []int, scope string) StackMetadata {
	return StackMetadata{
		StackSize: stackSize,
		PRNumbers: slices.Clone(prNumbers),
		Scope:     scope,
	}
}

// BuildStackMetadata derives stack metadata from merge branches.
func BuildStackMetadata(branches []MergeBranch, scope string) StackMetadata {
	prNumbers := make([]int, 0, len(branches))
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
			prStrs[i] = strconv.Itoa(n)
		}
		fmt.Fprintf(&b, "%s: %s\n", TrailerPRs, strings.Join(prStrs, ","))
	}

	if m.Scope != "" {
		fmt.Fprintf(&b, "%s: %s\n", TrailerScope, m.Scope)
	}

	return b.String()
}

// ParseStackMetadataTrailers extracts stack trailer values from a commit message body.
// Returns nil if no stack trailers are found.
func ParseStackMetadataTrailers(body string) *StackMetadata {
	info := &StackMetadata{}
	found := false

	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)

		if val, ok := parseTrailer(line, TrailerStackSize); ok {
			if n, err := strconv.Atoi(val); err == nil {
				info.StackSize = n
				found = true
			}
		}

		if val, ok := parseTrailer(line, TrailerPRs); ok {
			info.PRNumbers = parsePRNumbers(val)
			if len(info.PRNumbers) > 0 {
				found = true
			}
		}

		if val, ok := parseTrailer(line, TrailerScope); ok {
			info.Scope = val
			found = true
		}
	}

	if !found {
		return nil
	}
	return info
}

// parseTrailer checks if a line matches "Key: value" and returns the value.
func parseTrailer(line, key string) (string, bool) {
	prefix := key + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(line[len(prefix):]), true
}

// parsePRNumbers parses a comma-separated list of PR numbers.
func parsePRNumbers(s string) []int {
	var nums []int
	for part := range strings.SplitSeq(s, ",") {
		part = strings.TrimSpace(part)
		if n, err := strconv.Atoi(part); err == nil {
			nums = append(nums, n)
		}
	}
	return nums
}
