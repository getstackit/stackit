package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (r *runner) GetCurrentBranch() (string, error) {
	if name, ok := r.readHeadBranch(); ok {
		return name, nil
	}
	out, err := r.RunGitCommandWithContext(context.Background(), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("HEAD is not on a branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// readHeadBranch reads the checked-out branch straight out of the HEAD file,
// which is all `git symbolic-ref --short HEAD` does. The current branch is
// read constantly — it was the second-largest source of git processes in the
// suite — and a file read is orders of magnitude cheaper than a process.
//
// Reports ok=false for anything other than a plain refs/heads symref (detached
// HEAD, an unreadable file, a symref pointing outside refs/heads). The caller
// then runs the real command, so git stays the authority on every case except
// the overwhelmingly common one. Note this reads the worktree's own git dir,
// not the common dir: each worktree has its own HEAD.
func (r *runner) readHeadBranch() (string, bool) {
	if err := r.ensureRepo(); err != nil {
		return "", false
	}
	gitDir := r.getGitDir(context.Background())
	if gitDir == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	name, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: refs/heads/")
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// ResolveBranchNameCase reconciles a branch name reported by HEAD with the
// casing the ref actually carries in refs/heads.
//
// On case-insensitive filesystems (macOS and Windows defaults) loose refs
// `refs/heads/Claude/x` and `refs/heads/claude/x` are the same file, so
// whichever casing created the directory first wins on disk. HEAD, however,
// stores the name verbatim as requested at checkout time, so
// `git symbolic-ref --short HEAD` can echo back a casing that for-each-ref
// never reports. Every lookup of the current branch against the branch list
// then misses, and stackit reports the branch you are standing on as
// nonexistent.
//
// The exact match always wins, so this is a no-op on case-sensitive
// filesystems. The case-insensitive fallback engages only when the exact name
// is absent from branchNames and exactly one candidate differs by case alone —
// a case-insensitive filesystem cannot hold two such refs, so a unique match
// there is the ref HEAD meant.
//
// Caveat: on a case-sensitive filesystem an unborn branch (`git checkout
// --orphan Feature`) whose name differs only in case from an existing branch
// resolves to that existing branch. Harmless in practice, and the alternative
// costs a git call on every current-branch read.
func ResolveBranchNameCase(name string, branchNames []string) string {
	if name == "" || slices.Contains(branchNames, name) {
		return name
	}

	match := ""
	for _, candidate := range branchNames {
		if !strings.EqualFold(candidate, name) {
			continue
		}
		if match != "" {
			// Ambiguous: only possible on a case-sensitive filesystem, where
			// HEAD's casing is authoritative anyway.
			return name
		}
		match = candidate
	}
	if match == "" {
		return name
	}
	return match
}

// CaseMismatchHint returns a parenthetical explaining that name is absent from
// branchNames only because of casing, or "" when casing is not the problem.
// Turns "branch X does not exist" — reported for a branch the user can see —
// into a message that names both spellings.
func CaseMismatchHint(name string, branchNames []string) string {
	if resolved := ResolveBranchNameCase(name, branchNames); resolved != name {
		return fmt.Sprintf(" (a branch named %s exists; the names differ only in case, "+
			"which git cannot distinguish on case-insensitive filesystems)", resolved)
	}
	return ""
}

func (r *runner) GetAllBranchNames(ctx context.Context) ([]string, error) {
	branches, err := r.allBranchHashes(ctx)
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
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", "-m", "stackit: update branch ref", refName, sha); err != nil {
		return fmt.Errorf("failed to update branch ref: %w", err)
	}
	return nil
}

func (r *runner) UpdateBranchRefCAS(ctx context.Context, branchName, revision, expectedOld string) error {
	sha, err := r.resolveRefSHA(revision)
	if err != nil {
		return fmt.Errorf("failed to resolve revision %s: %w", revision, err)
	}
	if expectedOld == "" {
		return fmt.Errorf("expected old revision is required when updating branch %s", branchName)
	}
	if err := r.UpdateRefsBatch(ctx, []RefUpdate{{
		RefName: "refs/heads/" + branchName,
		NewSHA:  sha,
		OldSHA:  expectedOld,
	}}); err != nil {
		return fmt.Errorf("failed to compare-and-swap branch ref: %w", err)
	}
	return nil
}

func (r *runner) GetCurrentBranchOrSHA(ctx context.Context) (string, error) {
	branch, err := r.GetCurrentBranch()
	if err == nil {
		return branch, nil
	}
	return r.GetCurrentRevision(ctx)
}

// GetMergedBranches returns the set of local branches whose tip commit is
// reachable from target — i.e., branches that have been merged into target.
// Equivalent to running `isAncestor(branch, target)` for every branch, but
// does it in one `git for-each-ref --merged` invocation instead of N
// subprocesses.
//
// Squash-merged branches are intentionally not included: a squash leaves no
// ancestor relationship, so the per-branch isAncestor loop never reported them
// either. Callers that care about squash merges (engine_branch_status.go's
// deletion policy) consult PR metadata first and only fall back to this map.
func (r *runner) GetMergedBranches(ctx context.Context, target string) (map[string]bool, error) {
	out, err := r.RunGitCommandWithContext(ctx,
		"for-each-ref",
		"--format=%(refname:short)",
		"--merged", target,
		"refs/heads/",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches merged into %s: %w", target, err)
	}

	merged := make(map[string]bool)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		merged[line] = true
	}
	return merged, nil
}

// allBranchHashes returns a map of local branch name → SHA via a single
// `git for-each-ref` invocation.
func (r *runner) allBranchHashes(ctx context.Context) (map[string]string, error) {
	out, err := r.RunGitCommandWithContext(ctx,
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
