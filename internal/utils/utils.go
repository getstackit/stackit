// Package utils provides shared utility functions for the stackit codebase.
package utils

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mattn/go-isatty"
)

const (
	// MaxBranchNameByteLength is the maximum length for a branch name
	// Git refs have a max length of 256 bytes, minus 22 for "refs/stackit/metadata/"
	MaxBranchNameByteLength = 234
)

var (
	// BranchNameReplaceRegex matches characters that are not valid in branch names
	// Valid characters: letters, numbers, -, _, /, .
	BranchNameReplaceRegex = regexp.MustCompile(`[^-_/.a-zA-Z0-9]+`)

	// BranchNameIgnoreRegex matches trailing slashes and dots that should be removed
	BranchNameIgnoreRegex = regexp.MustCompile(`[/.]*$`)

	// interactiveMode is accessed atomically to avoid data races
	interactiveMode int32 = 1 // 1 = true, 0 = false
)

// SanitizeBranchName sanitizes a branch name by replacing invalid characters
func SanitizeBranchName(name string) string {
	// Remove trailing slashes and dots
	name = BranchNameIgnoreRegex.ReplaceAllString(name, "")

	// Replace invalid characters with hyphens
	name = BranchNameReplaceRegex.ReplaceAllString(name, "-")

	// Remove multiple consecutive hyphens
	hyphenRegex := regexp.MustCompile(`-+`)
	name = hyphenRegex.ReplaceAllString(name, "-")

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Limit length
	if len(name) > MaxBranchNameByteLength {
		name = name[:MaxBranchNameByteLength]
		// Trim trailing hyphen if we cut at a hyphen
		name = strings.TrimSuffix(name, "-")
	}

	return name
}

// GenerateBranchNameFromMessage generates a branch name from a commit message
func GenerateBranchNameFromMessage(message string) string {
	if message == "" {
		return ""
	}

	// Take first line of message (subject line)
	lines := strings.Split(message, "\n")
	subject := strings.TrimSpace(lines[0])

	// Remove common prefixes like "feat:", "fix:", etc. if present (with optional scope)
	subject = regexp.MustCompile(`^(feat|fix|chore|docs|style|refactor|perf|test|build|ci)(\([^)]+\))?:\s*`).ReplaceAllString(subject, "")

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
	return SanitizeBranchName(subject)
}

// ProcessBranchNamePattern processes a branch name pattern by replacing placeholders
// Supported placeholders:
//   - {username}: The sanitized Git username
//   - {date}: Current date and time in yyyyMMddHHmmss format in UTC
//   - {message}: The sanitized commit message subject (required)
//
// The pattern must contain {message} placeholder. The pattern is processed and then
// sanitized to ensure it's a valid branch name.
func ProcessBranchNamePattern(pattern string, username, date, message string) string {
	if pattern == "" {
		// If pattern is empty, just use the message (backward compatibility)
		return GenerateBranchNameFromMessage(message)
	}

	// Validate that pattern contains {message} placeholder
	if !strings.Contains(pattern, "{message}") {
		// Fallback to just the message if pattern doesn't contain {message}
		// This should not happen if validation in SetBranchNamePattern works correctly
		return GenerateBranchNameFromMessage(message)
	}

	// Extract message subject for {message} placeholder
	messageSubject := GenerateBranchNameFromMessage(message)

	// Replace placeholders
	result := pattern
	result = strings.ReplaceAll(result, "{username}", SanitizeBranchName(username))
	result = strings.ReplaceAll(result, "{date}", date)
	result = strings.ReplaceAll(result, "{message}", messageSubject)

	// Sanitize the final result
	return SanitizeBranchName(result)
}

// CleanCommitMessage removes comments and trailing whitespace from a commit message
func CleanCommitMessage(message string) string {
	lines := strings.Split(message, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r\n")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		result = append(result, trimmed)
	}

	// Remove trailing empty lines
	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	// Remove leading empty lines
	for len(result) > 0 && result[0] == "" {
		result = result[1:]
	}

	return strings.Join(result, "\n")
}

// SetInteractive sets whether the TUI should be interactive
func SetInteractive(interactive bool) {
	if interactive {
		atomic.StoreInt32(&interactiveMode, 1)
	} else {
		atomic.StoreInt32(&interactiveMode, 0)
	}
}

// IsTTY returns true if we can use a TTY for interactive TUI
func IsTTY() bool {
	if atomic.LoadInt32(&interactiveMode) == 0 {
		return false
	}
	// First check if stdin/stdout are terminals
	if !((isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())) &&
		(isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()))) {
		return false
	}
	// Also try to open /dev/tty to verify it's actually available
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsInteractive checks if we're in an interactive terminal
func IsInteractive() bool {
	return IsTTY()
}

// IsDemoMode returns true if STACKIT_DEMO environment variable is set
func IsDemoMode() bool {
	return os.Getenv("STACKIT_DEMO") != ""
}

// Run runs the given worker function for each item in the slice in parallel.
// It uses runtime.GOMAXPROCS(0) as the default number of workers.
func Run[T any](items []T, worker func(item T)) {
	if len(items) == 0 {
		return
	}

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > len(items) {
		numWorkers = len(items)
	}

	jobs := make(chan T, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for item := range jobs {
				worker(item)
			}
		}()
	}
	wg.Wait()
}

// RunWithWorkers runs the given worker function for each item in the slice in parallel with a specified number of workers.
func RunWithWorkers[T any](items []T, numWorkers int, worker func(item T)) {
	if len(items) == 0 {
		return
	}

	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}

	if numWorkers > len(items) {
		numWorkers = len(items)
	}

	jobs := make(chan T, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for item := range jobs {
				worker(item)
			}
		}()
	}
	wg.Wait()
}
