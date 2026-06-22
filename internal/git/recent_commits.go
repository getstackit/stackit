package git

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const trailerValueSeparator = "\x1e"

var (
	prNumberSuffixRe = regexp.MustCompile(`\(#(\d+)\)\s*$`)
	mergeSubjectRe   = regexp.MustCompile(`^Merge pull request #(\d+) from `)
	stackitTrailerRe = regexp.MustCompile(`^Stackit-[\w-]+:\s`)
)

// RecentCommitKind describes the presentation type of a trunk commit.
type RecentCommitKind string

const (
	RecentCommitKindRegular    RecentCommitKind = "regular"
	RecentCommitKindStackMerge RecentCommitKind = "stack-merge"
)

// RecentCommit represents a commit from the git log with optional stack trailer metadata.
type RecentCommit struct {
	SHA            string
	Subject        string
	Author         string
	Date           time.Time
	PRNumber       int              // parsed from subject suffix "(#123)" if present
	Kind           RecentCommitKind // derived from trailer metadata
	StackSize      int              // from Stackit-Stack-Size trailer (0 if absent)
	StackPRNumbers []int            // from Stackit-PRs trailer
	StackScope     string           // from Stackit-Scope trailer (empty if absent)
}

// commit log field separator used inside a record (never appears in commit
// metadata when escaped through git's pretty format).
const commitFieldSep = "\x1f"

// GetRecentCommits returns the most recent commits from a branch, including
// stack trailer metadata. For merge commits ("Merge pull request #N from ..."),
// the subject is replaced with the first line of the body, which contains the
// actual PR title.
//
// The underlying transport is `git log -z` so commit bodies (which may contain
// newlines) round-trip safely. Fields within a record are separated by US
// (0x1f); records by NUL.
func (r *runner) GetRecentCommits(ctx context.Context, branchName string, count int) ([]RecentCommit, error) {
	if count <= 0 {
		return nil, nil
	}
	return r.recentCommits(ctx, fmt.Sprintf("-n%d", count), branchName)
}

// GetRecentCommitsInRange returns the commits in a revision range (e.g.
// "v1.4.0..main"), newest first, with the same stack trailer parsing as
// GetRecentCommits. An empty range returns no commits.
func (r *runner) GetRecentCommitsInRange(ctx context.Context, revRange string) ([]RecentCommit, error) {
	if strings.TrimSpace(revRange) == "" {
		return nil, nil
	}
	return r.recentCommits(ctx, revRange)
}

// recentCommits runs `git log -z` with the trailer-aware format over the given
// log selector args (e.g. "-n10","main" or "v1.4.0..main") and parses the
// records into RecentCommit values. It is the shared core of GetRecentCommits
// and GetRecentCommitsInRange.
func (r *runner) recentCommits(ctx context.Context, revArgs ...string) ([]RecentCommit, error) {
	format := strings.Join([]string{
		"%H", "%an", "%aI", "%B",
	}, commitFieldSep)

	args := append([]string{"log"}, revArgs...)
	args = append(args, "-z", "--format="+format)

	out, err := r.RunGitCommandRawWithContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to walk recent commits (%v): %w", revArgs, err)
	}

	records := splitNulTerminated(out)
	if len(records) == 0 {
		return nil, nil
	}

	commits := make([]RecentCommit, 0, len(records))
	for _, rec := range records {
		fields := strings.SplitN(rec, commitFieldSep, 4)
		if len(fields) < 4 {
			return nil, fmt.Errorf("malformed git log record: %q", rec)
		}
		date, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[2]))
		if err != nil {
			return nil, fmt.Errorf("failed to parse commit date %q: %w", fields[2], err)
		}
		commits = append(commits, buildRecentCommit(fields[0], fields[1], date, fields[3]))
	}
	return commits, nil
}

// buildRecentCommit constructs a RecentCommit from the raw git log fields.
func buildRecentCommit(sha, author string, date time.Time, message string) RecentCommit {
	subject, body := splitCommitMessage(message)
	resolvedSubject := resolveSubject(subject, body)

	result := RecentCommit{
		SHA:     sha,
		Subject: resolvedSubject,
		Author:  author,
		Date:    date,
	}

	result.StackSize = parseStackSizeTrailer(stackitTrailerValues(message, "Stackit-Stack-Size"))
	result.StackPRNumbers = parseStackPRsTrailer(stackitTrailerValues(message, "Stackit-PRs"))
	result.StackScope = parseStackScopeTrailer(stackitTrailerValues(message, "Stackit-Scope"))

	result.PRNumber = parsePRNumberFromSubject(result.Subject)
	if result.PRNumber == 0 {
		result.PRNumber = parseMergePRNumber(subject)
	}
	result.Kind = deriveRecentCommitKind(result)
	return result
}

func splitCommitMessage(message string) (subject, body string) {
	message = strings.TrimRight(message, "\n")
	if message == "" {
		return "", ""
	}
	subject, body, _ = strings.Cut(message, "\n")
	body = strings.TrimLeft(body, "\n")
	return strings.TrimSpace(subject), body
}

func stackitTrailerValues(message, key string) string {
	var values []string
	for line := range strings.SplitSeq(message, "\n") {
		trailerKey, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(trailerKey) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, trailerValueSeparator)
}

// resolveSubject returns a human-readable subject for a commit.
// For GitHub merge commits ("Merge pull request #N from ..."), it extracts the
// actual PR title from the first line of the body.
func resolveSubject(subject, body string) string {
	if !mergeSubjectRe.MatchString(subject) {
		return subject
	}
	firstLine := firstNonEmptyLine(body)
	if firstLine != "" {
		return firstLine
	}
	return subject
}

// parseMergePRNumber extracts the PR number from a "Merge pull request #N from ..." subject.
func parseMergePRNumber(subject string) int {
	matches := mergeSubjectRe.FindStringSubmatch(subject)
	if len(matches) < 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}

func firstNonEmptyLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip Stackit trailer lines (e.g. "Stackit-Stack-Size: 11")
		if stackitTrailerRe.MatchString(line) {
			continue
		}
		return line
	}
	return ""
}

func parseStackSizeTrailer(raw string) int {
	for value := range strings.SplitSeq(raw, trailerValueSeparator) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return 0
}

func parseStackPRsTrailer(raw string) []int {
	var prNumbers []int
	for value := range strings.SplitSeq(raw, trailerValueSeparator) {
		for part := range strings.SplitSeq(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err != nil || slices.Contains(prNumbers, n) {
				continue
			}
			prNumbers = append(prNumbers, n)
		}
	}
	return prNumbers
}

func parseStackScopeTrailer(raw string) string {
	for value := range strings.SplitSeq(raw, trailerValueSeparator) {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func parsePRNumberFromSubject(subject string) int {
	matches := prNumberSuffixRe.FindStringSubmatch(subject)
	if len(matches) < 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}

func deriveRecentCommitKind(commit RecentCommit) RecentCommitKind {
	if commit.StackSize > 0 {
		return RecentCommitKindStackMerge
	}
	return RecentCommitKindRegular
}
