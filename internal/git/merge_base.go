package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func (r *runner) getMergeBaseByRef(ctx context.Context, ref1Name, ref2Name string) (string, error) {
	out, err := r.RunGitCommandWithContext(ctx, "merge-base", ref1Name, ref2Name)
	if err != nil {
		return "", fmt.Errorf("failed to find merge base of %s and %s: %w", ref1Name, ref2Name, err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("no merge base found")
	}
	return sha, nil
}

func (r *runner) isAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	_, err := r.RunGitCommandWithContext(ctx, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	// `git merge-base --is-ancestor` exits 1 when the answer is "no". Any
	// other non-zero exit is a real error (bad SHA, repo missing, etc.).
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("failed to check ancestry of %s..%s: %w", ancestor, descendant, err)
}
