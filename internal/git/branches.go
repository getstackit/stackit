package git

import (
	"context"
	"fmt"
	"slices"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
)

func (r *runner) GetCurrentBranch() (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}

	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	if !head.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is not on a branch")
	}
	return head.Name().Short(), nil
}

func (r *runner) GetAllBranchNames() ([]string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return nil, err
	}
	branches, err := r.allBranchHashes(repo)
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
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to check status before checkout: %w", err)
	}
	err = worktree.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Keep:   !status.IsClean(),
	})
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) CheckoutBranchForce(ctx context.Context, branchName string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()
	err = worktree.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Force:  true,
	})
	return err
}

func (r *runner) CreateAndCheckoutBranch(ctx context.Context, branchName string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to check status before creating branch %s: %w", branchName, err)
	}
	err = worktree.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: true,
		Keep:   !status.IsClean(),
	})
	if err != nil {
		return fmt.Errorf("failed to create and checkout branch %s: %w", branchName, err)
	}
	return nil
}

func (r *runner) DeleteBranch(ctx context.Context, branchName string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()
	err = repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(branchName))
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}
	r.revisionCache.Delete(branchName)
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
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	refName := plumbing.NewBranchReferenceName(branchName)
	if _, err := repo.Reference(refName, false); err == nil {
		return fmt.Errorf("failed to create branch %s from %s: branch already exists", branchName, startPoint)
	}

	hash, err := r.resolveRefHashInternal(repo, startPoint)
	if err != nil {
		return fmt.Errorf("failed to create branch %s from %s: %w", branchName, startPoint, err)
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
		return fmt.Errorf("failed to create branch %s from %s: %w", branchName, startPoint, err)
	}
	return nil
}

func (r *runner) CreateBranchForce(ctx context.Context, branchName, revision string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, revision)
	if err != nil {
		return err
	}

	refName := plumbing.NewBranchReferenceName(branchName)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
		return err
	}
	r.revisionCache.Delete(branchName)
	return nil
}

func (r *runner) CheckoutDetached(ctx context.Context, revision string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, revision)
	if err == nil {
		status, statusErr := worktree.Status()
		if statusErr != nil {
			return fmt.Errorf("failed to check status before detached checkout: %w", statusErr)
		}
		err = worktree.Checkout(&gogit.CheckoutOptions{Hash: hash, Keep: !status.IsClean()})
	}
	if err != nil {
		return fmt.Errorf("failed to checkout %s in detached state: %w", revision, err)
	}
	return nil
}

func (r *runner) UpdateBranchRef(ctx context.Context, branchName, revision string) error {
	repo, err := r.ensureRepo()
	if err != nil {
		return err
	}

	r.goGitMu.Lock()
	defer r.goGitMu.Unlock()

	hash, err := r.resolveRefHashInternal(repo, revision)
	if err != nil {
		return fmt.Errorf("failed to resolve revision %s: %w", revision, err)
	}

	refName := plumbing.NewBranchReferenceName(branchName)
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
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
	repo, err := r.ensureRepo()
	if err != nil {
		return nil, err
	}

	targetHash, err := r.resolveRefHash(repo, target)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target %s: %w", target, err)
	}

	branches, err := r.allBranchHashes(repo)
	if err != nil {
		return nil, err
	}

	merged := make(map[string]bool)
	for branchName, branchHash := range branches {
		if branchHash == targetHash {
			merged[branchName] = true
			continue
		}
		isMerged, err := r.isAncestorGoGit(repo, branchHash, targetHash)
		if err != nil {
			return nil, fmt.Errorf("failed to check if %s is merged into %s: %w", branchName, target, err)
		}
		if isMerged {
			merged[branchName] = true
		}
	}
	return merged, nil
}

func (r *runner) allBranchHashes(repo *Repository) (map[string]plumbing.Hash, error) {
	iter, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to get branches: %w", err)
	}
	defer iter.Close()

	branches := make(map[string]plumbing.Hash)
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			branches[ref.Name().Short()] = ref.Hash()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate branches: %w", err)
	}
	return branches, nil
}
