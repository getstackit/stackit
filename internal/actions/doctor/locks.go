package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// staleLockThreshold is how old a git lock file must be before doctor treats it
// as left over by a crashed or killed process rather than a live operation. A
// hung `git fetch` (issue #1330) leaves exactly this kind of lock, which is why
// "restarting the machine released the lock."
const staleLockThreshold = 5 * time.Minute

// lockFileNames are the well-known single-file locks git takes during ref,
// index, config, and gc operations.
var lockFileNames = []string{
	"index.lock",
	"HEAD.lock",
	"ORIG_HEAD.lock",
	"config.lock",
	"packed-refs.lock",
	"shallow.lock",
	"gc.pid",
}

// checkGitLocks reports stale git lock files in the repository. It is a purely
// local, read-only filesystem scan — no git process is spawned — so it stays in
// the local-first part of the repository checks. A stale lock blocks every
// subsequent git operation, so surfacing it lets a user recover without a
// reboot.
func checkGitLocks(repoRoot string, handler Handler, warnings int) int {
	var stale, fresh []string
	for _, p := range gitLockPaths(repoRoot) {
		info, err := os.Stat(p)
		if err != nil {
			continue // not present (the common, healthy case)
		}
		name := lockDisplayName(repoRoot, p)
		if age := time.Since(info.ModTime()); age >= staleLockThreshold {
			stale = append(stale, fmt.Sprintf("%s (%s old)", name, age.Round(time.Second)))
		} else {
			fresh = append(fresh, name)
		}
	}

	switch {
	case len(stale) > 0:
		warnings++
		handler.OnCheck("git_locks", CheckWarning, fmt.Sprintf(
			"stale git lock file(s) — remove them if no git process is running: %s",
			strings.Join(stale, ", ")))
	case len(fresh) > 0:
		handler.OnCheck("git_locks", CheckPassed, fmt.Sprintf(
			"git lock file(s) present but recent (an operation may be in progress): %s",
			strings.Join(fresh, ", ")))
	default:
		handler.OnCheck("git_locks", CheckPassed, "No stale git lock files")
	}
	return warnings
}

// gitLockPaths returns every candidate lock path for the repository: the
// well-known single-file locks in both the worktree and shared git dirs, plus
// any per-ref *.lock files under refs/.
func gitLockPaths(repoRoot string) []string {
	gitDir := git.GetGitDir(repoRoot)
	commonDir := git.GetGitCommonDir(repoRoot)

	dirs := []string{gitDir}
	if commonDir != gitDir {
		dirs = append(dirs, commonDir)
	}

	paths := make([]string, 0, len(dirs)*len(lockFileNames))
	for _, dir := range dirs {
		for _, name := range lockFileNames {
			paths = append(paths, filepath.Join(dir, name))
		}
	}

	// Per-ref locks live next to the loose refs in the shared dir. Unreadable
	// subtrees are skipped (err != nil) rather than failing the whole check.
	refsDir := filepath.Join(commonDir, "refs")
	_ = filepath.WalkDir(refsDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".lock") {
			paths = append(paths, p)
		}
		return nil
	})
	return paths
}

// lockDisplayName renders a lock path relative to the repo root when possible,
// so messages read as ".git/index.lock" rather than an absolute path.
func lockDisplayName(repoRoot, p string) string {
	if rel, err := filepath.Rel(repoRoot, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
