package git

import (
	"context"
	"fmt"
	"os"
	"slices"
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

// StashCapture holds a stash and the index flags that a commit cannot encode.
type StashCapture struct {
	SHA              string
	IntentToAddPaths []string
}

// StashCreate records the working tree and index as a stash commit without
// touching either and without pushing an entry onto the stash list. A clean
// working tree produces no commit, reported as an empty SHA. Intent-to-add
// paths are staged only in a temporary index and returned separately so callers
// can reinstate their flags with RestoreIntentToAdd after applying the stash.
//
// The commit it returns is unreachable, so callers that need it to survive
// longer than the current command must anchor it under a ref.
func (r *runner) StashCreate(ctx context.Context, message string) (StashCapture, error) {
	args := []string{gitCmdStash, "create"}
	if message != "" {
		args = append(args, message)
	}
	output, err := r.RunGitCommandWithContext(ctx, args...)
	if err == nil {
		return StashCapture{SHA: strings.TrimSpace(output)}, nil
	}

	// Git cannot stash an index containing intent-to-add entries. Retry with
	// those files staged in a temporary index, preserving the real index and
	// its staged/unstaged split. Save the flags separately for restoration.
	pathsOutput, pathsErr := r.RunGitCommandRawWithContext(ctx, "diff", "--no-renames", "--name-only", "--diff-filter=A", "--ita-invisible-in-index", "-z", "--")
	paths := splitNulTerminated(pathsOutput)
	if pathsErr != nil || len(paths) == 0 {
		return StashCapture{}, fmt.Errorf("stash create failed: %w", err)
	}
	indexTree, err := r.RunGitCommandWithContext(ctx, "write-tree")
	if err != nil {
		return StashCapture{}, fmt.Errorf("failed to capture index tree: %w", err)
	}
	index, err := os.CreateTemp("", "stackit-stash-index-")
	if err != nil {
		return StashCapture{}, err
	}
	defer func() { _ = os.Remove(index.Name()) }()
	if err := index.Close(); err != nil {
		return StashCapture{}, err
	}
	env := []string{"GIT_INDEX_FILE=" + index.Name()}
	if _, err := r.RunGitCommandWithEnv(ctx, env, "read-tree", strings.TrimSpace(indexTree)); err != nil {
		return StashCapture{}, fmt.Errorf("failed to initialize capture index: %w", err)
	}
	for chunk := range slices.Chunk(paths, restorePathBatchSize) {
		addArgs := append([]string{gitLiteralPathspecs, gitCmdAdd, "--force", "--"}, chunk...)
		if _, err := r.RunGitCommandWithEnv(ctx, env, addArgs...); err != nil {
			return StashCapture{}, fmt.Errorf("failed to capture intent-to-add files: %w", err)
		}
	}
	output, err = r.RunGitCommandWithEnv(ctx, env, args...)
	if err != nil {
		return StashCapture{}, fmt.Errorf("stash create with intent-to-add failed: %w", err)
	}
	return StashCapture{SHA: strings.TrimSpace(output), IntentToAddPaths: paths}, nil
}

// RestoreIntentToAdd reinstates flags after applying a capture made with those
// paths temporarily staged. Reset changes only the index, leaving content alone.
func (r *runner) RestoreIntentToAdd(ctx context.Context, paths []string) error {
	for chunk := range slices.Chunk(paths, restorePathBatchSize) {
		args := append([]string{gitLiteralPathspecs, "reset", "-N", "HEAD", "--"}, chunk...)
		if _, err := r.RunGitCommandWithContext(ctx, args...); err != nil {
			return fmt.Errorf("failed to restore intent-to-add flags: %w", err)
		}
	}
	return nil
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
