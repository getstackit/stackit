// Package common provides shared helper functions for CLI commands.
package common

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Restack reason constants used by sync and get handlers.
const (
	ReasonNoRestackNeeded = "does not need restacking"
	ReasonLocked          = "is locked"
	ReasonFrozen          = "is frozen"
)

// FormatPRInfo formats a PR number for display.
// Returns empty string if prNumber is nil.
func FormatPRInfo(prNumber *int) string {
	if prNumber == nil {
		return ""
	}
	return fmt.Sprintf(" (PR #%d)", *prNumber)
}

// BranchResultStatus represents the status of a branch operation.
type BranchResultStatus int

const (
	// StatusDone indicates the operation completed successfully.
	StatusDone BranchResultStatus = iota
	// StatusError indicates the operation failed.
	StatusError
)

// BranchResult holds data for formatting branch operation results.
type BranchResult struct {
	BranchName string
	Status     BranchResultStatus
	Output     string
	Error      error
	IsCurrent  bool
}

// FormatBranchSummary formats a list of branch results for display.
// Returns the count of successful and failed operations.
func FormatBranchSummary(out output.Output, results []BranchResult) (success, fail int) {
	for _, result := range results {
		switch result.Status {
		case StatusDone:
			success++
			out.Info("  ✓ %s", style.ColorBranchNameIf(result.BranchName, result.IsCurrent))
			printIndentedOutput(result.Output, out.Info)
		case StatusError:
			fail++
			out.Error("  ✗ %s", style.ColorBranchNameIf(result.BranchName, result.IsCurrent))
			if result.Error != nil {
				out.Error("    Error: %v", result.Error)
			}
			printIndentedOutput(result.Output, out.Info)
		}
	}
	return
}

// printIndentedOutput prints output lines with indentation.
func printIndentedOutput(text string, logFn func(string, ...any)) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	for line := range strings.SplitSeq(trimmed, "\n") {
		if strings.TrimSpace(line) != "" {
			logFn("    %s", line)
		}
	}
}
