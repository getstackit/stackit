// Package branchname sanitizes, validates, and generates Git branch names.
package branchname

import (
	"regexp"
	"strings"
)

const (
	// MaxByteLength is the maximum length for a branch name.
	// Git refs have a max length of 256 bytes, minus 22 for "refs/stackit/metadata/"
	MaxByteLength = 234

	// errTrailingDotOrSlash is the error reason for branch names ending in . or /
	errTrailingDotOrSlash = "cannot end with a dot or slash"
)

var (
	// invalidCharsRegex matches characters that are not valid in branch names.
	// Valid characters: letters, numbers, -, _, /, .
	invalidCharsRegex = regexp.MustCompile(`[^-_/.a-zA-Z0-9]+`)

	// trailingDotsSlashesRegex matches trailing slashes and dots that should be removed
	trailingDotsSlashesRegex = regexp.MustCompile(`[/.]*$`)

	// hyphenRunRegex matches consecutive hyphens for collapsing
	hyphenRunRegex = regexp.MustCompile(`-+`)

	// conventionalCommitPrefixRegex matches conventional commit prefixes like "feat:", "fix(scope):", etc.
	conventionalCommitPrefixRegex = regexp.MustCompile(`^(feat|fix|chore|docs|style|refactor|perf|test|build|ci)(\([^)]+\))?:\s*`)
)

// Sanitize turns arbitrary text into a valid branch name by replacing invalid characters
func Sanitize(name string) string {
	// Remove trailing slashes and dots
	name = trailingDotsSlashesRegex.ReplaceAllString(name, "")

	// Replace invalid characters with hyphens
	name = invalidCharsRegex.ReplaceAllString(name, "-")

	// Remove multiple consecutive hyphens
	name = hyphenRunRegex.ReplaceAllString(name, "-")

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Limit length
	if len(name) > MaxByteLength {
		name = name[:MaxByteLength]
		// Trim trailing hyphen if we cut at a hyphen
		name = strings.TrimSuffix(name, "-")
	}

	return name
}

// Validate checks if a branch name is valid according to Git rules.
// Returns an error describing the issue if invalid, nil if valid.
func Validate(name string) error {
	if name == "" {
		return &InvalidError{Name: name, Reason: "branch name cannot be empty"}
	}

	// Check for invalid characters
	if invalidCharsRegex.MatchString(name) {
		return &InvalidError{Name: name, Reason: "contains invalid characters (only letters, numbers, -, _, /, . are allowed)"}
	}

	// Check for consecutive dots (Git disallows "..")
	if strings.Contains(name, "..") {
		return &InvalidError{Name: name, Reason: "cannot contain consecutive dots (..)"}
	}

	// Check for leading hyphen
	if strings.HasPrefix(name, "-") {
		return &InvalidError{Name: name, Reason: "cannot start with a hyphen"}
	}

	// Check for trailing dot or slash
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, "/") {
		return &InvalidError{Name: name, Reason: errTrailingDotOrSlash}
	}

	// Check for leading slash
	if strings.HasPrefix(name, "/") {
		return &InvalidError{Name: name, Reason: "cannot start with a slash"}
	}

	// Check for consecutive slashes
	if strings.Contains(name, "//") {
		return &InvalidError{Name: name, Reason: "cannot contain consecutive slashes"}
	}

	// Check length
	if len(name) > MaxByteLength {
		return &InvalidError{Name: name, Reason: "exceeds maximum length of 234 characters"}
	}

	return nil
}

// InvalidError reports why a branch name is invalid.
type InvalidError struct {
	Name   string
	Reason string
}

func (e *InvalidError) Error() string {
	return "invalid branch name \"" + e.Name + "\": " + e.Reason
}

// FromMessage generates a branch name from a commit message subject.
func FromMessage(message string) string {
	if message == "" {
		return ""
	}

	// Take first line of message (subject line)
	firstLine, _, _ := strings.Cut(message, "\n")
	subject := strings.TrimSpace(firstLine)

	// Remove common prefixes like "feat:", "fix:", etc. if present (with optional scope)
	subject = conventionalCommitPrefixRegex.ReplaceAllString(subject, "")

	// Truncate to a reasonable length for branch names (before sanitization)
	// Aim for ~50 characters to leave room for username/date prefixes
	maxSubjectLength := 50
	if len(subject) > maxSubjectLength {
		// Try to truncate at word boundary
		truncated := subject[:maxSubjectLength]
		lastSpace := strings.LastIndex(truncated, " ")
		if lastSpace > maxSubjectLength/2 {
			// If we can find a space in the second half, truncate there
			subject = truncated[:lastSpace]
		} else {
			// Otherwise just truncate
			subject = truncated
		}
	}

	// Sanitize and return
	return Sanitize(subject)
}
