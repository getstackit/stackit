package git

import (
	"context"
	"fmt"
	"strings"
)

func (r *runner) StashPush(ctx context.Context, message string) (string, error) {
	args := []string{gitCmdStash, gitCmdPush, "-u"}
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
	if err := r.requireGitVersion(ctx, Version{Major: 2, Minor: 35}, "git stash --staged"); err != nil {
		return "", err
	}

	args := []string{gitCmdStash, gitCmdPush, "--staged"}
	if message != "" {
		args = append(args, "-m", message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("stash push --staged failed: %w", err)
	}
	return output, nil
}

// StashDrop drops the stash entry at ref. It refuses an empty ref: bare
// `git stash drop` would silently target stash@{0}, which may be an
// unrelated user stash.
func (r *runner) StashDrop(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("stash drop requires an explicit stash ref")
	}
	if _, err := r.RunGitCommandWithContext(ctx, gitCmdStash, "drop", ref); err != nil {
		return fmt.Errorf("stash drop failed: %w", err)
	}
	return nil
}

func (r *runner) StashPop(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, gitCmdStash, "pop")
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
	if _, err := r.RunGitCommandWithContext(ctx, gitCmdStash, "pop", ref); err != nil {
		return fmt.Errorf("stash pop failed: %w", err)
	}
	return nil
}

func (r *runner) ListStash(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, gitCmdStash, "list")
}

// StashApplyMode selects how a stash entry is put back onto the working tree.
type StashApplyMode int

const (
	// StashApplyWithIndex restores the staged/unstaged split the stash was
	// created with.
	StashApplyWithIndex StashApplyMode = iota
	// StashApplyWorktreeOnly brings every change back as an unstaged
	// modification.
	StashApplyWorktreeOnly
)

// StashCreate records the working tree and index as a stash commit without
// touching either and without pushing an entry onto the stash list. A clean
// working tree produces no commit, reported as an empty SHA.
//
// The commit it returns is unreachable, so callers that need it to survive
// longer than the current command must anchor it under a ref.
func (r *runner) StashCreate(ctx context.Context, message string) (string, error) {
	args := []string{gitCmdStash, "create"}
	if message != "" {
		args = append(args, message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("stash create failed: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// StashApplyRef applies the stash entry at ref, which may be a stash-list
// reference or a raw stash commit, and leaves the entry in place. It refuses an
// empty ref: bare `git stash apply` would silently target stash@{0}, which may
// be an unrelated user stash.
func (r *runner) StashApplyRef(ctx context.Context, ref string, mode StashApplyMode) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("stash apply requires an explicit stash ref")
	}
	args := []string{gitCmdStash, "apply"}
	if mode == StashApplyWithIndex {
		args = append(args, "--index")
	}
	args = append(args, ref)
	if _, err := r.RunGitCommandWithContext(ctx, args...); err != nil {
		return fmt.Errorf("stash apply failed: %w", err)
	}
	return nil
}
