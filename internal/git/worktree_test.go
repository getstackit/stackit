package git_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestTreeContainsAnyPathLargePathSet(t *testing.T) {
	t.Parallel()

	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("tracked", "tracked")
	})
	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

	// This must remain a known, non-collision result. An arbitrary path-count
	// cap used to turn it into "unknown", which holds the branch and all of its
	// validated-plan descendants despite none of the files being at risk.
	// One over the old cap, plus room for the colliding path appended below.
	paths := make([]string, 201, 202)
	for i := range paths {
		paths[i] = fmt.Sprintf("untracked-%03d.txt", i)
	}
	collides, known := runner.TreeContainsAnyPath(context.Background(), "HEAD", paths)
	require.True(t, known)
	require.False(t, collides)

	paths = append(paths, "tracked_test.txt")
	collides, known = runner.TreeContainsAnyPath(context.Background(), "HEAD", paths)
	require.True(t, known)
	require.True(t, collides)
}

// TestWorktreeResetBlocker pins the untracked-collision-vs-coexistence boundary
// required by .claude/rules/worktree-safety.md. Blocking on any untracked file
// at all stops a ref move for the ordinary state of having written a new file
// and not staged it, which broke `sync` from a managed worktree.
func TestWorktreeResetBlocker(t *testing.T) {
	t.Parallel()

	// Build an "incoming" commit containing incoming_test.txt, then rewind the
	// worktree so that path is absent and can stand in as an untracked file.
	scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("base", "base")
	})
	repo := scene.Repo
	baseRev, err := repo.GetCurrentSHA()
	require.NoError(t, err)
	require.NoError(t, repo.CreateChangeAndCommit("incoming", "incoming"))
	incomingRev, err := repo.GetCurrentSHA()
	require.NoError(t, err)
	require.NoError(t, repo.RunGitCommand("reset", "--hard", baseRev))

	runner := git.NewRunnerWithPath(repo.Dir, nil)
	ctx := context.Background()
	scratch := filepath.Join(repo.Dir, "scratch.txt")
	collider := filepath.Join(repo.Dir, "incoming_test.txt")

	// Each phase mutates the shared worktree, so they run in order rather than
	// as parallel subtests.
	require.Empty(t, runner.WorktreeResetBlocker(ctx, repo.Dir, incomingRev),
		"a clean worktree must not block")

	require.NoError(t, os.WriteFile(scratch, []byte("notes"), 0o600))
	require.Empty(t, runner.WorktreeResetBlocker(ctx, repo.Dir, incomingRev),
		"an untracked file the incoming commit does not write must not block")
	require.NoError(t, os.Remove(scratch))

	require.NoError(t, os.WriteFile(collider, []byte("mine"), 0o600))
	require.Equal(t, "an untracked file there would be overwritten",
		runner.WorktreeResetBlocker(ctx, repo.Dir, incomingRev),
		"an untracked file the incoming commit writes is the one case reset destroys")
	require.NoError(t, os.Remove(collider))

	require.NoError(t, os.WriteFile(filepath.Join(repo.Dir, "base_test.txt"), []byte("edited"), 0o600))
	require.Equal(t, "has uncommitted changes",
		runner.WorktreeResetBlocker(ctx, repo.Dir, incomingRev),
		"a tracked change must block unconditionally")
}

func TestWorktree(t *testing.T) {
	t.Run("lists ignored files", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		require.NoError(t, os.WriteFile(filepath.Join(scene.Repo.Dir, ".gitignore"), []byte(".env\ncache/\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(scene.Repo.Dir, ".env"), []byte("secret"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(scene.Repo.Dir, "cache"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(scene.Repo.Dir, "cache", "data"), []byte("cached"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(scene.Repo.Dir, "tracked.txt"), []byte("tracked"), 0o600))

		ignored, err := runner.ListIgnoredFiles(context.Background(), scene.Repo.Dir)
		require.NoError(t, err)
		require.Equal(t, []string{".env", "cache/data"}, ignored)
	})

	t.Run("add and remove worktree", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Create a branch to checkout in the worktree
		err := scene.Repo.CreateBranch("test-branch")
		require.NoError(t, err)

		// Create a temporary directory for the worktree
		tmpDir := t.TempDir()

		// Normalize worktree path (on macOS /var is symlinked to /private/var)
		worktreePath, err := filepath.EvalSymlinks(tmpDir)
		require.NoError(t, err)
		worktreePath = filepath.Join(worktreePath, "worktree")

		// Add worktree
		err = runner.AddWorktree(context.Background(), worktreePath, "test-branch", git.WorktreeAttached)
		require.NoError(t, err)

		// Verify worktree exists
		_, err = os.Stat(filepath.Join(worktreePath, ".git"))
		require.NoError(t, err)

		// List worktrees
		worktrees, err := runner.ListWorktrees(context.Background())
		require.NoError(t, err)
		require.Contains(t, worktrees.Paths(), worktreePath)

		// Remove worktree
		err = runner.RemoveWorktree(context.Background(), worktreePath)
		require.NoError(t, err)

		// Verify worktree is gone from list
		worktrees, err = runner.ListWorktrees(context.Background())
		require.NoError(t, err)
		require.NotContains(t, worktrees.Paths(), worktreePath)
	})

	t.Run("add detached worktree", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Create a temporary directory for the worktree
		tmpDir := t.TempDir()

		worktreePath := filepath.Join(tmpDir, "worktree-detached")

		// Add detached worktree
		err := runner.AddWorktree(context.Background(), worktreePath, "", git.WorktreeDetached)
		require.NoError(t, err)

		// Verify worktree exists
		_, err = os.Stat(filepath.Join(worktreePath, ".git"))
		require.NoError(t, err)

		// Clean up
		err = runner.RemoveWorktree(context.Background(), worktreePath)
		require.NoError(t, err)
	})
}

func TestIsMainWorktree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	root, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	tests := []struct {
		name         string
		worktreePath string
		repoRoot     string
		expected     bool
	}{
		{
			name:         "same path is main worktree",
			worktreePath: root,
			repoRoot:     root,
			expected:     true,
		},
		{
			name:         "different path is not main worktree",
			worktreePath: filepath.Join(root, "linked"),
			repoRoot:     root,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, git.IsMainWorktree(tt.worktreePath, tt.repoRoot))
		})
	}
}

func TestWorktreeRegistry(t *testing.T) {
	t.Run("removes reverse path registration after symlinked worktree is gone", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		targetBase := t.TempDir()
		linkBase := filepath.Join(t.TempDir(), "linked-base")
		require.NoError(t, os.Symlink(targetBase, linkBase))
		worktreePath := filepath.Join(linkBase, "worktree")

		require.NoError(t, runner.WriteWorktreeMeta(context.Background(), "first", &git.WorktreeMeta{
			Path:         worktreePath,
			AnchorBranch: "first",
		}))
		// Match real cleanup ordering: by this point the worktree directory is
		// gone, while its symlinked base still exists.
		require.NoError(t, runner.DeleteWorktreeMeta(context.Background(), "first"))

		// Re-registration at the same path must not collide with an orphaned
		// reverse path ref hashed with a different spelling.
		require.NoError(t, runner.WriteWorktreeMeta(context.Background(), "second", &git.WorktreeMeta{
			Path:         worktreePath,
			AnchorBranch: "second",
		}))
	})

	// A registration whose metadata blob cannot be parsed must still be
	// deletable. Propagating the read error made the ref undeletable through
	// every stackit path that retires one — remove, detach, prune, and sync's
	// orphan cleanup — leaving a manual `git update-ref -d` as the only exit
	// from a state the repair work in this area exists to recover from.
	t.Run("deletes a registration whose metadata cannot be parsed", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		require.NoError(t, runner.WriteWorktreeMeta(context.Background(), "corrupt", &git.WorktreeMeta{
			Path:         filepath.Join(t.TempDir(), "worktree"),
			AnchorBranch: "corrupt",
		}))

		// Point the registration at a blob that is not valid metadata.
		garbage, err := runner.CreateBlob("this is not json")
		require.NoError(t, err)
		require.NoError(t, runner.UpdateRef("refs/stackit/worktrees/corrupt", garbage))

		_, readErr := runner.ReadWorktreeMeta("corrupt")
		require.Error(t, readErr, "precondition: metadata must be unreadable")

		require.NoError(t, runner.DeleteWorktreeMeta(context.Background(), "corrupt"))

		meta, err := runner.ReadWorktreeMeta("corrupt")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("write and read worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write worktree metadata
		meta := &git.WorktreeMeta{
			Path:         "/path/to/worktree",
			AnchorBranch: "feature-branch",
			MainRepoDir:  scene.Repo.Dir,
		}
		err := runner.WriteWorktreeMeta(context.Background(), "feature-branch", meta)
		require.NoError(t, err)

		// Read it back
		readMeta, err := runner.ReadWorktreeMeta("feature-branch")
		require.NoError(t, err)
		require.NotNil(t, readMeta)
		require.Equal(t, "/path/to/worktree", readMeta.Path)
		require.Equal(t, "feature-branch", readMeta.AnchorBranch)
		require.Equal(t, scene.Repo.Dir, readMeta.MainRepoDir)
	})

	t.Run("read non-existent worktree metadata returns nil", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Read non-existent metadata
		meta, err := runner.ReadWorktreeMeta("non-existent")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("delete worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write worktree metadata
		meta := &git.WorktreeMeta{
			Path:         "/path/to/worktree",
			AnchorBranch: "feature-branch",
		}
		err := runner.WriteWorktreeMeta(context.Background(), "feature-branch", meta)
		require.NoError(t, err)

		// Delete it
		err = runner.DeleteWorktreeMeta(context.Background(), "feature-branch")
		require.NoError(t, err)

		// Verify it's gone
		readMeta, err := runner.ReadWorktreeMeta("feature-branch")
		require.NoError(t, err)
		require.Nil(t, readMeta)
	})

	t.Run("list worktree metadata", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Write multiple worktree metadata
		meta1 := &git.WorktreeMeta{
			Path:         "/path/to/worktree1",
			AnchorBranch: "feature-1",
		}
		err := runner.WriteWorktreeMeta(context.Background(), "feature-1", meta1)
		require.NoError(t, err)

		meta2 := &git.WorktreeMeta{
			Path:         "/path/to/worktree2",
			AnchorBranch: "feature-2",
		}
		err = runner.WriteWorktreeMeta(context.Background(), "feature-2", meta2)
		require.NoError(t, err)

		// List all
		metas, err := runner.ListWorktreeMetas()
		require.NoError(t, err)
		require.Len(t, metas, 2)
		require.Contains(t, metas, "feature-1")
		require.Contains(t, metas, "feature-2")
		require.Equal(t, "/path/to/worktree1", metas["feature-1"].Path)
		require.Equal(t, "/path/to/worktree2", metas["feature-2"].Path)
	})
}

// TestWorktreeListIsMain covers the guard that stops a cleanup path from asking
// git to remove the user's main working tree.
//
// The regression: the guard compared the candidate path against the *engine's*
// repo root. A consolidation merge runs its steps with an engine rooted in a
// temporary worktree, so the main checkout never matched, the guard passed, and
// `git worktree remove <main repo>` was attempted (git refuses, but only after
// the branch deletion that followed had already been derailed).
func TestWorktreeListIsMain(t *testing.T) {
	t.Parallel()

	mainDir := t.TempDir()
	linkedDir := t.TempDir()
	list := git.WorktreeList{
		{Path: mainDir, Branch: "main"},
		{Path: linkedDir, Branch: "feature"},
	}

	t.Run("first entry is the main worktree", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, mainDir, list.MainPath())
		require.True(t, list.IsMain(mainDir))
		require.False(t, list.IsMain(linkedDir))
	})

	t.Run("main worktree is recognized regardless of any repo root", func(t *testing.T) {
		t.Parallel()
		// This is the actual bug: an unrelated directory standing in for the
		// temporary merge worktree's repo root must not change the answer.
		require.False(t, git.IsMainWorktree(mainDir, t.TempDir()))
		require.True(t, list.IsMain(mainDir))
	})

	t.Run("unresolvable paths are treated as main", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "gone")
		// Cannot prove it is not the main worktree, so removal must not proceed.
		require.True(t, list.IsMain(missing))
		require.True(t, git.WorktreeList{}.IsMain(mainDir))
	})

	t.Run("two unresolvable paths are not equal", func(t *testing.T) {
		t.Parallel()
		// Both EvalSymlinks calls used to be discarded, so "" == "" reported a
		// false positive match between any two nonexistent paths.
		require.False(t, git.IsMainWorktree(
			filepath.Join(t.TempDir(), "gone-a"),
			filepath.Join(t.TempDir(), "gone-b"),
		))
	})
}
