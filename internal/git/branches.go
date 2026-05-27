package git

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func (r *runner) GetCurrentBranch() (string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("HEAD is not on a branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (r *runner) GetAllBranchNames() ([]string, error) {
	branches, err := r.allBranchHashes()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

func (r *runner) CheckoutBranch(ctx context.Context, branchName string) error {
	if _, err := r.RunGitCommandWithContext(ctx, "checkout", branchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) CheckoutBranchForce(ctx context.Context, branchName string) error {
	if _, err := r.RunGitCommandWithContext(ctx, "checkout", "--force", branchName); err != nil {
		return fmt.Errorf("failed to force-checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) CreateAndCheckoutBranch(ctx context.Context, branchName string) error {
	if _, err := r.RunGitCommandWithContext(ctx, "checkout", "-b", branchName); err != nil {
		return fmt.Errorf("failed to create and checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) DeleteBranch(ctx context.Context, branchName string) error {
	refName := fmt.Sprintf("refs/heads/%s", branchName)

	if err := r.ensureBranchesNotCheckedOut(ctx, []string{branchName}); err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}

	// `git update-ref -d` rewrites packed-refs correctly and is lenient when
	// the ref doesn't exist.
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", "-d", refName); err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}

	if err := r.verifyRefDeleted(ctx, refName); err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}

	r.revisionCache.Delete(branchName)
	return nil
}

// verifyRefDeleted returns an error if the ref still resolves after a delete.
// Defense against silent failures in the underlying storage layer (e.g. when a
// ref deletion appears to succeed but a packed entry survives).
func (r *runner) verifyRefDeleted(ctx context.Context, refName string) error {
	_, err := r.RunGitCommandRawWithContext(ctx, "show-ref", "--verify", "--quiet", refName)
	if err == nil {
		return fmt.Errorf("ref %s still resolves after delete", refName)
	}
	return nil
}

func (r *runner) RenameBranch(ctx context.Context, oldName, newName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "branch", "-m", oldName, newName)
	if err != nil {
		return fmt.Errorf("failed to rename branch %s to %s: %w", oldName, newName, err)
	}
	r.revisionCache.Delete(oldName)
	r.revisionCache.Delete(newName)
	return nil
}

func (r *runner) CreateBranch(ctx context.Context, branchName, startPoint string) error {
	refName := fmt.Sprintf("refs/heads/%s", branchName)

	// Existence check via show-ref — exit 0 means the ref exists; exit 1 means
	// it doesn't (or anything else, which we map to "doesn't exist" for the
	// purpose of the precondition).
	if _, err := r.RunGitCommandRawWithContext(ctx, "show-ref", "--verify", "--quiet", refName); err == nil {
		return fmt.Errorf("failed to create branch %s from %s: branch already exists", branchName, startPoint)
	}

	sha, err := r.resolveRefSHA(startPoint)
	if err != nil {
		return fmt.Errorf("failed to create branch %s from %s: %w", branchName, startPoint, err)
	}

	// update-ref with no old-value asserts "ref must not exist"; combined with
	// the show-ref pre-check above this is race-resistant enough for stackit's
	// usage (single-process at a time on a given repo).
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", refName, sha); err != nil {
		return fmt.Errorf("failed to create branch %s from %s: %w", branchName, startPoint, err)
	}
	return nil
}

func (r *runner) CreateBranchForce(ctx context.Context, branchName, revision string) error {
	sha, err := r.resolveRefSHA(revision)
	if err != nil {
		return err
	}
	refName := fmt.Sprintf("refs/heads/%s", branchName)
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", refName, sha); err != nil {
		return fmt.Errorf("failed to force-create branch %s at %s: %w", branchName, revision, err)
	}
	r.revisionCache.Delete(branchName)
	return nil
}

func (r *runner) CheckoutDetached(ctx context.Context, revision string) error {
	if _, err := r.RunGitCommandWithContext(ctx, "checkout", "--detach", revision); err != nil {
		return fmt.Errorf("failed to checkout %s in detached state: %w", revision, err)
	}
	return nil
}

func (r *runner) UpdateBranchRef(ctx context.Context, branchName, revision string) error {
	sha, err := r.resolveRefSHA(revision)
	if err != nil {
		return fmt.Errorf("failed to resolve revision %s: %w", revision, err)
	}
	refName := fmt.Sprintf("refs/heads/%s", branchName)
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", refName, sha); err != nil {
		return fmt.Errorf("failed to update branch ref: %w", err)
	}
	r.revisionCache.Delete(branchName)
	return nil
}

func (r *runner) GetCurrentBranchOrSHA(ctx context.Context) (string, error) {
	branch, err := r.GetCurrentBranch()
	if err == nil {
		return branch, nil
	}
	return r.GetCurrentRevision(ctx)
}

func (r *runner) GetMergedBranches(_ context.Context, target string) (map[string]bool, error) {
	targetSHA, err := r.resolveRefSHA(target)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	branches, err := r.allBranchHashes()
	if err != nil {
		return nil, err
	}

	merged := make(map[string]bool)
	for branchName, branchSHA := range branches {
		if branchSHA == targetSHA {
			merged[branchName] = true
			continue
		}
		isMerged, err := r.isAncestor(branchSHA, targetSHA)
		if err != nil {
			return nil, fmt.Errorf("failed to check if %s is merged into %s: %w", branchName, target, err)
		}
		if isMerged {
			merged[branchName] = true
		}
	}
	return merged, nil
}

// allBranchHashes returns a map of local branch name → SHA via a single
// `git for-each-ref` invocation.
func (r *runner) allBranchHashes() (map[string]string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(),
		"for-each-ref",
		"--format=%(refname:short)%00%(objectname)",
		"refs/heads/",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	branches := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		name, sha, ok := strings.Cut(line, "\x00")
		if !ok || sha == "" {
			continue
		}
		branches[name] = sha
	}
	return branches, nil
}
