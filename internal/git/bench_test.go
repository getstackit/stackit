package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/getstackit/stackit/internal/git"
)

// benchRepo holds a temp repo used by benchmarks. The repo is initialized with
// a baseline configuration that avoids global git config (so numbers are
// reproducible across machines) and a runner pointed at it.
type benchRepo struct {
	dir    string
	runner git.Runner
}

// newBenchRepo creates a temp git repo with `commits` commits on main and
// `branches` extra branches each carrying one additional commit off main.
// Returns a runner pointed at the repo. Cleanup is registered via b.Cleanup.
//
// The repo is created fresh per benchmark to avoid cross-bench state leakage;
// b.ResetTimer is the caller's responsibility after any per-iteration setup.
func newBenchRepo(b *testing.B, commits, branches int) *benchRepo {
	b.Helper()
	dir := b.TempDir()

	runGit := func(args ...string) {
		b.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	if err := exec.Command("git", "-c", "init.defaultBranch=main", "init", dir, "-b", "main").Run(); err != nil {
		b.Fatalf("git init failed: %v", err)
	}
	runGit("config", "user.name", "Bench User")
	runGit("config", "user.email", "bench@example.com")

	writeAndCommit := func(name, content, message string) {
		b.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			b.Fatalf("write %s: %v", name, err)
		}
		runGit("add", name)
		runGit("commit", "-m", message)
	}

	writeAndCommit("seed.txt", "seed", "initial commit")
	for i := 1; i < commits; i++ {
		writeAndCommit("seed.txt", fmt.Sprintf("seed-%d", i), fmt.Sprintf("commit %d", i))
	}

	for i := range branches {
		name := fmt.Sprintf("branch-%d", i)
		runGit("checkout", "-b", name)
		writeAndCommit(fmt.Sprintf("file-%d.txt", i), fmt.Sprintf("branch %d content", i), fmt.Sprintf("branch %d", i))
		runGit("checkout", "main")
	}

	runner := git.NewRunnerWithPath(dir, nil)
	return &benchRepo{dir: dir, runner: runner}
}

// runGit shells out a git command in the bench repo, failing the bench on error.
func (br *benchRepo) runGit(b *testing.B, args ...string) {
	b.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = br.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// BenchmarkGetRecentCommits measures the cost of walking N recent commits.
// Phase 2 replaces the go-git CommitObject walk with `git log --format=...`.
func BenchmarkGetRecentCommits(b *testing.B) {
	for _, count := range []int{50, 200} {
		b.Run(fmt.Sprintf("count=%d", count), func(b *testing.B) {
			br := newBenchRepo(b, count+5, 0)
			for b.Loop() {
				if _, err := br.runner.GetRecentCommits("main", count); err != nil {
					b.Fatalf("GetRecentCommits: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetRevision measures the cost of resolving a branch name to a SHA.
// Phase 2 replaces the go-git Reference lookup with `git rev-parse --verify`.
// The revisionCache may serve the read on subsequent calls; this bench
// measures the cache-warm path. See BenchmarkGetRevisionCold for the miss path.
func BenchmarkGetRevision(b *testing.B) {
	br := newBenchRepo(b, 5, 1)
	if _, err := br.runner.GetRevision("branch-0"); err != nil {
		b.Fatalf("warm GetRevision: %v", err)
	}
	for b.Loop() {
		if _, err := br.runner.GetRevision("branch-0"); err != nil {
			b.Fatalf("GetRevision: %v", err)
		}
	}
}

// BenchmarkBatchGetRevisions measures bulk revision resolution. The bench
// repo has 20 branches; LoadAllBranchRevisions warms the cache via a single
// go-git iteration today (Phase 2 swaps that for a single `git for-each-ref`).
func BenchmarkBatchGetRevisions(b *testing.B) {
	const branches = 20
	br := newBenchRepo(b, 5, branches)
	names := make([]string, branches)
	for i := range names {
		names[i] = fmt.Sprintf("branch-%d", i)
	}
	for b.Loop() {
		if _, errs := br.runner.BatchGetRevisions(names); len(errs) > 0 {
			b.Fatalf("BatchGetRevisions: %v", errs[0])
		}
	}
}

// BenchmarkLoadAllBranchRevisions measures the cache-preload path used by
// engine startup. Today this is a go-git Branches() iteration under goGitMu.
func BenchmarkLoadAllBranchRevisions(b *testing.B) {
	br := newBenchRepo(b, 5, 20)
	for b.Loop() {
		if err := br.runner.LoadAllBranchRevisions(); err != nil {
			b.Fatalf("LoadAllBranchRevisions: %v", err)
		}
	}
}

// BenchmarkGetMergeBase exercises the merge-base API. Phase 2 replaces
// go-git's commit.MergeBase walk with `git merge-base <a> <b>`.
func BenchmarkGetMergeBase(b *testing.B) {
	br := newBenchRepo(b, 30, 2)
	for b.Loop() {
		if _, err := br.runner.GetMergeBase("branch-0", "branch-1"); err != nil {
			b.Fatalf("GetMergeBase: %v", err)
		}
	}
}

// BenchmarkIsAncestor exercises ancestry checks. Phase 2 replaces
// commit.IsAncestor with `git merge-base --is-ancestor` (cheap plumbing).
func BenchmarkIsAncestor(b *testing.B) {
	br := newBenchRepo(b, 30, 1)
	ancestorSHA, err := br.runner.RunGitCommandWithContext(context.Background(), "rev-parse", "main~5")
	if err != nil {
		b.Fatalf("resolve ancestor sha: %v", err)
	}
	descendantSHA, err := br.runner.RunGitCommandWithContext(context.Background(), "rev-parse", "main")
	if err != nil {
		b.Fatalf("resolve descendant sha: %v", err)
	}
	for b.Loop() {
		if _, err := br.runner.IsAncestor(ancestorSHA, descendantSHA); err != nil {
			b.Fatalf("IsAncestor: %v", err)
		}
	}
}

// BenchmarkHasStagedChanges exercises the go-git Worktree.Status() path used
// by stackit's pre-commit guards. Phase 3 replaces it with
// `git status --porcelain=v2 -z`.
func BenchmarkHasStagedChanges(b *testing.B) {
	br := newBenchRepo(b, 5, 0)
	for b.Loop() {
		if _, err := br.runner.HasStagedChanges(context.Background()); err != nil {
			b.Fatalf("HasStagedChanges: %v", err)
		}
	}
}

// BenchmarkHasStagedChanges_LargeTree measures the worktree walk on a tree
// with many untracked files. This is the worst case for the current go-git
// Status() implementation, which walks the entire tree in-process.
func BenchmarkHasStagedChanges_LargeTree(b *testing.B) {
	br := newBenchRepo(b, 5, 0)
	for i := range 1000 {
		path := filepath.Join(br.dir, fmt.Sprintf("untracked-%04d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			b.Fatalf("write untracked: %v", err)
		}
	}
	for b.Loop() {
		if _, err := br.runner.HasStagedChanges(context.Background()); err != nil {
			b.Fatalf("HasStagedChanges: %v", err)
		}
	}
}

// BenchmarkStageAll measures the cost of staging all changes via
// worktree.AddWithOptions today; Phase 3 swaps in `git add -A`.
func BenchmarkStageAll(b *testing.B) {
	iter := 0
	for b.Loop() {
		b.StopTimer()
		br := newBenchRepo(b, 5, 0)
		path := filepath.Join(br.dir, "new.txt")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("iter-%d", iter)), 0o600); err != nil {
			b.Fatalf("write: %v", err)
		}
		iter++
		b.StartTimer()

		if err := br.runner.StageAll(context.Background()); err != nil {
			b.Fatalf("StageAll: %v", err)
		}
	}
}

// BenchmarkGetConfig measures single-key config reads. Today this goes
// through go-git's format.Config parser; Phase 5 swaps in `git config <key>`.
// Expect a regression on this bench unless a small in-process cache is added.
func BenchmarkGetConfig(b *testing.B) {
	br := newBenchRepo(b, 1, 0)
	br.runGit(b, "config", "stackit.bench.key", "bench-value")
	for b.Loop() {
		if _, err := br.runner.GetConfig("stackit.bench.key"); err != nil {
			b.Fatalf("GetConfig: %v", err)
		}
	}
}

// BenchmarkSetConfig measures config writes. Today this is read-modify-write
// of the parsed config file; Phase 5 swaps in `git config <key> <value>`.
func BenchmarkSetConfig(b *testing.B) {
	br := newBenchRepo(b, 1, 0)
	iter := 0
	for b.Loop() {
		if err := br.runner.SetConfig("stackit.bench.iter", fmt.Sprintf("v%d", iter)); err != nil {
			b.Fatalf("SetConfig: %v", err)
		}
		iter++
	}
}

// BenchmarkCreateBlob measures single-blob writes. Today these go through
// repo.Storer.SetEncodedObject (in-process). Phase 6 swaps in
// `git hash-object -w --stdin`. Expect a regression for singletons offset by
// batched writes for stack-wide metadata.
func BenchmarkCreateBlob(b *testing.B) {
	for _, size := range []int{256, 4096} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			br := newBenchRepo(b, 1, 0)
			content := make([]byte, size)
			for i := range content {
				content[i] = byte('a' + (i % 26))
			}
			s := string(content)
			for b.Loop() {
				if _, err := br.runner.CreateBlob(s); err != nil {
					b.Fatalf("CreateBlob: %v", err)
				}
			}
		})
	}
}

// BenchmarkCreateBlobs_Sequential simulates the metadata-flush pattern in
// engine_writer.go (BatchMarkNeedsPRBodyUpdate writes one blob per branch in
// a loop). Phase 6 replaces this with WriteBlobsBatch.
func BenchmarkCreateBlobs_Sequential(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("blobs=%d", n), func(b *testing.B) {
			br := newBenchRepo(b, 1, 0)
			iter := 0
			for b.Loop() {
				for j := range n {
					content := fmt.Sprintf("blob-iter-%d-idx-%d", iter, j)
					if _, err := br.runner.CreateBlob(content); err != nil {
						b.Fatalf("CreateBlob: %v", err)
					}
				}
				iter++
			}
		})
	}
}

// BenchmarkReadBlob measures blob reads. Phase 6 swaps in `git cat-file blob`.
func BenchmarkReadBlob(b *testing.B) {
	br := newBenchRepo(b, 1, 0)
	sha, err := br.runner.CreateBlob("read-bench-content")
	if err != nil {
		b.Fatalf("CreateBlob: %v", err)
	}
	for b.Loop() {
		if _, err := br.runner.ReadBlob(sha); err != nil {
			b.Fatalf("ReadBlob: %v", err)
		}
	}
}

// BenchmarkParallelGetRevision is the goGitMu-sensitive case: N goroutines
// resolving distinct branch names concurrently. Today the goGitMu serializes
// the underlying go-git reference lookup; after Phase 2 + Phase 6, parallel
// callers should scale near-linearly.
func BenchmarkParallelGetRevision(b *testing.B) {
	const branches = 20
	br := newBenchRepo(b, 5, branches)
	names := make([]string, branches)
	for i := range names {
		names[i] = fmt.Sprintf("branch-%d", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := names[i%branches]
			i++
			if _, err := br.runner.GetRevision(name); err != nil {
				b.Fatalf("GetRevision: %v", err)
			}
		}
	})
}

// BenchmarkParallelGetMergeBase mirrors BenchmarkParallelGetRevision for
// merge-base. Today merge-base also takes goGitMu (the inner CommitObject
// walk requires it). Native `git merge-base` is parallel-safe.
func BenchmarkParallelGetMergeBase(b *testing.B) {
	br := newBenchRepo(b, 30, 4)
	pairs := [][2]string{
		{"branch-0", "branch-1"},
		{"branch-1", "branch-2"},
		{"branch-2", "branch-3"},
		{"branch-3", "branch-0"},
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			p := pairs[i%len(pairs)]
			i++
			if _, err := br.runner.GetMergeBase(p[0], p[1]); err != nil {
				b.Fatalf("GetMergeBase: %v", err)
			}
		}
	})
}
