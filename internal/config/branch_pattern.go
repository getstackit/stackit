// Package config provides repository configuration management,
// including reading and writing stackit configuration files.
package config

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

// BranchPattern represents a branch name pattern with validation
type BranchPattern string

// DefaultBranchPattern is the default branch name pattern
const DefaultBranchPattern BranchPattern = "{username}/{date}/{message}"

// placeholderRegex matches {placeholder} tokens in a branch pattern.
var placeholderRegex = regexp.MustCompile(`\{[^}]+\}`)

// NewBranchPattern creates a new BranchPattern from a string
// Returns an error if the pattern is invalid (doesn't contain {message})
func NewBranchPattern(pattern string) (BranchPattern, error) {
	if pattern == "" {
		return DefaultBranchPattern, nil
	}

	// Validate that pattern contains {message} placeholder
	if !strings.Contains(pattern, "{message}") {
		return "", fmt.Errorf("branch name pattern must contain {message} placeholder")
	}

	return BranchPattern(pattern), nil
}

// String returns the string representation of the pattern
func (p BranchPattern) String() string {
	if p == "" {
		return string(DefaultBranchPattern)
	}
	return string(p)
}

// IsValid checks if the pattern is valid (contains {message})
func (p BranchPattern) IsValid() bool {
	return strings.Contains(string(p), "{message}")
}

// ContainsScope returns true if the pattern contains the {scope} placeholder
func (p BranchPattern) ContainsScope() bool {
	return strings.Contains(p.String(), "{scope}")
}

// WithDefault returns the pattern, or the default if empty
func (p BranchPattern) WithDefault() BranchPattern {
	if p == "" {
		return DefaultBranchPattern
	}
	return p
}

// GitContext is a minimal interface that provides the git user name and a
// context. This matches app.Context but avoids a circular dependency and keeps
// branch-name generation from depending on the full git runner.
type GitContext interface {
	context.Context
	GetUserName(ctx context.Context) (string, error)
}

// GetBranchName generates a branch name from the pattern using the provided commit message and optional scope.
// It fetches the username and current date internally only if needed by the pattern.
func (p BranchPattern) GetBranchName(ctx GitContext, commitMessage string, scope string) (string, error) {
	pattern := p.String()
	if pattern == "" {
		// If pattern is empty, just use the message (backward compatibility)
		branchName := utils.GenerateBranchNameFromMessage(commitMessage)
		if branchName == "" {
			return "", fmt.Errorf("failed to generate branch name from commit message")
		}
		return branchName, nil
	}

	// Define all available placeholder replacement functions
	placeholderFuncs := map[string]func() string{
		"{username}": func() string {
			username, err := ctx.GetUserName(ctx)
			if err != nil {
				// If we can't get username, use empty string (will be sanitized)
				return ""
			}
			return utils.SanitizeBranchName(username)
		},
		"{date}": git.GetCurrentDate,
		"{message}": func() string {
			return utils.GenerateBranchNameFromMessage(commitMessage)
		},
		"{scope}": func() string {
			return utils.SanitizeBranchName(scope)
		},
	}

	// Scan pattern once to find which placeholders are present
	// Use regex to find all {placeholder} patterns in one pass
	foundPlaceholders := make(map[string]bool)
	for _, match := range placeholderRegex.FindAllString(pattern, -1) {
		foundPlaceholders[match] = true
	}

	// Validate that pattern contains {message} placeholder
	if !foundPlaceholders["{message}"] {
		// Fallback to just the message if pattern doesn't contain {message}
		branchName := utils.GenerateBranchNameFromMessage(commitMessage)
		if branchName == "" {
			return "", fmt.Errorf("failed to generate branch name from commit message")
		}
		return branchName, nil
	}

	// Build map of replacements for found placeholders only
	replacements := make(map[string]func() string)
	for placeholder, replacementFn := range placeholderFuncs {
		if foundPlaceholders[placeholder] {
			replacements[placeholder] = replacementFn
		}
	}

	// Apply replacements in sequence
	result := pattern
	for placeholder, replacementFn := range replacements {
		result = strings.ReplaceAll(result, placeholder, replacementFn())
	}

	// Sanitize the final result
	branchName := utils.SanitizeBranchName(result)
	if branchName == "" {
		return "", fmt.Errorf("failed to generate branch name from commit message")
	}

	return branchName, nil
}
