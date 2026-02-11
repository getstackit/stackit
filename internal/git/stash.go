package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func (r *runner) StashPush(ctx context.Context, message string) (string, error) {
	args := []string{"stash", gitCmdPush, "-u"}
	if message != "" {
		args = append(args, "-m", message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("stash push failed: %w", err)
	}
	return output, nil
}

// StashPushStaged stashes only the currently staged changes, leaving unstaged changes in the working tree.
// This is useful for temporarily saving staged work while keeping other modifications.
// Note: The --staged flag requires Git 2.35 or later.
func (r *runner) StashPushStaged(ctx context.Context, message string) (string, error) {
	// Check Git version first - --staged requires Git 2.35+
	if !r.isGitVersionAtLeast(ctx, 2, 35) {
		return "", fmt.Errorf("git stash --staged requires Git 2.35 or later; please upgrade your Git installation")
	}

	args := []string{"stash", gitCmdPush, "--staged"}
	if message != "" {
		args = append(args, "-m", message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("stash push --staged failed: %w", err)
	}
	return output, nil
}

// isGitVersionAtLeast checks if the installed Git version is at least major.minor.
// The version is cached after the first check to avoid repeated git --version calls.
func (r *runner) isGitVersionAtLeast(ctx context.Context, major, minor int) bool {
	r.gitVersionOnce.Do(func() {
		r.parseGitVersion(ctx)
	})

	if !r.gitVersionParsed {
		return false // Assume old version if parsing failed
	}

	if r.gitVersionMajor > major {
		return true
	}
	if r.gitVersionMajor == major && r.gitVersionMinor >= minor {
		return true
	}
	return false
}

// parseGitVersion parses and caches the Git version.
func (r *runner) parseGitVersion(ctx context.Context) {
	output, err := r.RunGitCommandWithContext(ctx, "--version")
	if err != nil {
		return // Leave gitVersionParsed as false
	}

	// Parse "git version X.Y.Z" format
	// Examples: "git version 2.39.2", "git version 2.35.1.windows.2"
	re := regexp.MustCompile(`git version (\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(strings.TrimSpace(output))
	if len(matches) < 3 {
		return
	}

	gitMajor, err := strconv.Atoi(matches[1])
	if err != nil {
		return
	}
	gitMinor, err := strconv.Atoi(matches[2])
	if err != nil {
		return
	}

	r.gitVersionMajor = gitMajor
	r.gitVersionMinor = gitMinor
	r.gitVersionParsed = true
}

// StashDrop drops the stash entry at ref. It refuses an empty ref: bare
// `git stash drop` would silently target stash@{0}, which may be an
// unrelated user stash.
func (r *runner) StashDrop(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("stash drop requires an explicit stash ref")
	}
	if _, err := r.RunGitCommandWithContext(ctx, "stash", "drop", ref); err != nil {
		return fmt.Errorf("stash drop failed: %w", err)
	}
	return nil
}

func (r *runner) StashPop(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "stash", "pop")
	if err != nil {
		return fmt.Errorf("stash pop failed: %w", err)
	}
	return nil
}

// StashPopRef pops the stash entry at ref. It refuses an empty ref: bare
// `git stash pop` would silently target stash@{0}, which may be an
// unrelated user stash.
func (r *runner) StashPopRef(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("stash pop requires an explicit stash ref")
	}
	if _, err := r.RunGitCommandWithContext(ctx, "stash", "pop", ref); err != nil {
		return fmt.Errorf("stash pop failed: %w", err)
	}
	return nil
}

func (r *runner) ListStash(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, "stash", "list")
}
