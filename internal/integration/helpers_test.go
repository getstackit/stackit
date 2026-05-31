package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/utils"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/inprocess"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// =============================================================================
// TestShell Options - Builder Pattern for Test Configuration
// =============================================================================

// TestShellOptions configures how a TestShell is created.
type TestShellOptions struct {
	withRemote bool
}

// TestShellOption is a functional option for configuring TestShell creation.
type TestShellOption func(*TestShellOptions)

// WithRemote configures the TestShell to include a local bare repository
// as "origin" remote. Use this for tests that need push/pull/fetch/sync.
// Without this option, no remote is set up, which is faster for tests
// that only need local operations.
func WithRemote() TestShellOption {
	return func(opts *TestShellOptions) {
		opts.withRemote = true
	}
}

func init() {
	scenario.SetGlobalInProcessRunner(func(workDir string, args ...string) (string, error) {
		runner := inprocess.NewInProcessCLI()
		res := runner.Run(workDir, args...)
		return res.Output, res.Err
	})
}

// =============================================================================
// Test Shell - A helper to make integration tests read like terminal sessions
// =============================================================================

// TestShell wraps a test scene and provides a fluent interface for running
// commands. Tests using this read like a series of terminal commands.
type TestShell struct {
	t            *testing.T
	scene        *testhelpers.Scene
	binaryPath   string
	lastOutput   string
	inProcessCLI *inprocess.CLI // if set, use in-process execution
}

// NewTestShell creates a shell-like test environment with an initialized repo.
func NewTestShell(t *testing.T, binaryPath string) *TestShell {
	t.Helper()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	return &TestShell{t: t, scene: scene, binaryPath: binaryPath}
}

// NewTestShellInProcess creates a shell-like test environment that uses in-process
// CLI execution for faster tests. This avoids the overhead of spawning a new process
// for each command (~8ms per command savings).
//
// Options:
//   - WithRemote(): Set up a local bare repository as "origin" for tests needing
//     push/pull/fetch/sync operations. Without this, no remote is configured.
//
// Usage:
//
//	sh := NewTestShellInProcess(t)              // No remote (fast)
//	sh := NewTestShellInProcess(t, WithRemote()) // With remote
func NewTestShellInProcess(t *testing.T, opts ...TestShellOption) *TestShell {
	t.Helper()

	// Apply options
	options := &TestShellOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// If remote is requested, delegate to the with-remote implementation
	if options.withRemote {
		sh := newTestShellWithRemote(t, "", inprocess.NewInProcessCLI())
		// Force non-interactive mode for in-process execution
		utils.SetInteractive(false)
		sh.ensureTrunkConfig()
		return sh
	}

	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	sh := &TestShell{
		t:            t,
		scene:        scene,
		inProcessCLI: inprocess.NewInProcessCLI(),
	}
	// Force non-interactive mode for in-process execution to match behavior
	// of spawning binary with --no-interactive
	utils.SetInteractive(false)
	// Pre-seed git config to avoid per-test init overhead.
	sh.ensureTrunkConfig()
	return sh
}

// NewTestShellWithRemote creates a shell-like test environment with a local bare repo as "origin".
// This is useful for testing sync workflows that require a remote.
func NewTestShellWithRemote(t *testing.T, binaryPath string) *TestShell {
	t.Helper()
	return newTestShellWithRemote(t, binaryPath, nil)
}

// newTestShellWithRemote is the shared implementation for creating shells with remotes.
func newTestShellWithRemote(t *testing.T, binaryPath string, inProcessCLI *inprocess.CLI) *TestShell {
	t.Helper()
	scene := testhelpers.NewRemoteSceneParallel(t)
	return &TestShell{t: t, scene: scene, binaryPath: binaryPath, inProcessCLI: inProcessCLI}
}

// ensureTrunkConfig sets the trunk config directly to avoid calling `stackit init`
// for every test. This keeps tests fast and still marks the repo as initialized.
func (s *TestShell) ensureTrunkConfig() {
	s.t.Helper()
	// Default trunk branch for test repos is main.
	require.NoError(s.t, s.scene.Repo.RunGitCommand("config", "--local", "stackit.trunk", "main"))
}

// Scene returns the underlying test scene for direct access when needed.
func (s *TestShell) Scene() *testhelpers.Scene {
	return s.scene
}

// WithT returns a shallow copy of the shell that reports assertions against the provided test.
func (s *TestShell) WithT(t *testing.T) *TestShell {
	t.Helper()
	return &TestShell{
		t:            t,
		scene:        s.scene,
		binaryPath:   s.binaryPath,
		inProcessCLI: s.inProcessCLI,
	}
}

// Dir returns the working directory of the test shell.
func (s *TestShell) Dir() string {
	return s.scene.Dir
}

// =============================================================================
// Command Execution
// =============================================================================

// Run executes a stackit CLI command (e.g., "create feature-a -m 'Add feature'")
func (s *TestShell) Run(args string) *TestShell {
	s.t.Helper()
	parts := splitArgs(args)

	// Use in-process execution if available (faster)
	if s.inProcessCLI != nil {
		result := s.inProcessCLI.Run(s.scene.Dir, parts...)
		s.lastOutput = ansi.Strip(result.Output)
		if result.Err != nil {
			s.t.Logf("In-process CLI output: %s", s.lastOutput)
		}
		require.NoError(s.t, result.Err, "$ stackit %s\n%s", args, s.lastOutput)
		return s
	}

	// Fall back to process-based execution
	// Always run with --no-interactive in tests
	fullArgs := append([]string{"--no-interactive"}, parts...)
	cmd := exec.Command(s.binaryPath, fullArgs...)
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	s.lastOutput = ansi.Strip(string(output))
	require.NoError(s.t, err, "$ stackit %s\n%s", args, s.lastOutput)
	return s
}

// RunExpectError executes a stackit CLI command and expects it to fail.
func (s *TestShell) RunExpectError(args string) *TestShell {
	s.t.Helper()
	parts := splitArgs(args)

	// Use in-process execution if available (faster)
	if s.inProcessCLI != nil {
		result := s.inProcessCLI.Run(s.scene.Dir, parts...)
		s.lastOutput = ansi.Strip(result.Output)
		require.Error(s.t, result.Err, "$ stackit %s (expected error)\n%s", args, s.lastOutput)
		return s
	}

	// Fall back to process-based execution
	// Always run with --no-interactive in tests
	fullArgs := append([]string{"--no-interactive"}, parts...)
	cmd := exec.Command(s.binaryPath, fullArgs...)
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	s.lastOutput = ansi.Strip(string(output))
	require.Error(s.t, err, "$ stackit %s (expected error)\n%s", args, s.lastOutput)
	return s
}

// Git executes a raw git command (use sparingly - prefer stackit commands)
func (s *TestShell) Git(args string) *TestShell {
	s.t.Helper()
	parts := splitArgs(args)
	cmd := exec.Command("git", parts...)
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	s.lastOutput = string(output)
	require.NoError(s.t, err, "$ git %s\n%s", args, s.lastOutput)
	return s
}

// =============================================================================
// Navigation Shortcuts
// =============================================================================

// Checkout switches to a branch using stackit checkout
func (s *TestShell) Checkout(branch string) *TestShell {
	s.t.Helper()
	return s.Run("checkout " + branch)
}

// Top navigates to the top of the current stack
func (s *TestShell) Top() *TestShell {
	s.t.Helper()
	return s.Run("top")
}

// Bottom navigates to the bottom of the current stack
func (s *TestShell) Bottom() *TestShell {
	s.t.Helper()
	return s.Run("bottom")
}

// =============================================================================
// File Operations
// =============================================================================

// Write creates/modifies a file and stages it (simulates editing a file)
func (s *TestShell) Write(filename, content string) *TestShell {
	s.t.Helper()
	err := s.scene.Repo.CreateChange(content, filename, false)
	require.NoError(s.t, err, "failed to write %s", filename)
	return s
}

// WriteFile creates/modifies a file with the exact filename and stages it
func (s *TestShell) WriteFile(filename, content string) *TestShell {
	s.t.Helper()
	filePath := filepath.Join(s.scene.Dir, filename)
	err := os.WriteFile(filePath, []byte(content), 0644)
	require.NoError(s.t, err, "failed to write file %s", filename)
	return s.Git("add " + filename)
}

// SetWorktreeBasePath sets the worktree base path for this repo.
func (s *TestShell) SetWorktreeBasePath(path string) *TestShell {
	s.t.Helper()
	path = canonicalPath(path)
	return s.Git("config --local stackit.worktree.basePath " + path)
}

// ResetRepo resets the repo to a clean state for reuse across scenarios.
// This removes worktrees, stackit refs, extra branches, and resets main to the root commit.
func (s *TestShell) ResetRepo() *TestShell {
	s.t.Helper()

	// Remove stale index.lock files that can be left behind by interrupted git operations.
	lockPath := filepath.Join(s.scene.Dir, ".git", "index.lock")
	_ = os.Remove(lockPath)

	// Clean any leftover working tree changes before switching branches.
	s.Git("reset --hard")
	s.Git("clean -fd")
	s.Git("checkout -f main")
	s.Git("rev-parse --show-toplevel")
	mainPath := canonicalPath(strings.TrimSpace(s.lastOutput))

	// Remove all worktrees except the main worktree
	s.Git("worktree list --porcelain")
	for _, path := range parseWorktreePaths(s.lastOutput) {
		if path != "" && !samePath(path, mainPath) && !isMainWorktree(path) {
			s.Git("worktree remove --force " + path)
		}
	}
	// Prune any stale worktrees
	s.Git("worktree prune")

	// Delete stackit refs (metadata, local metadata, worktree registrations)
	s.Git("for-each-ref --format=%(refname) refs/stackit")
	for _, ref := range splitLines(s.lastOutput) {
		if ref != "" {
			s.Git("update-ref -d " + ref)
		}
	}

	// Delete local branches except main
	s.Git("for-each-ref --format=%(refname:short) refs/heads")
	for _, branch := range splitLines(s.lastOutput) {
		if branch != "" && branch != "main" {
			s.Git("branch -D " + branch)
		}
	}

	// Reset main back to the root commit
	s.Git("rev-list --max-parents=0 main")
	root := strings.TrimSpace(s.lastOutput)
	if root != "" {
		s.Git("reset --hard " + root)
	}
	s.Git("clean -fd")

	s.resetOrigin(root)
	return s
}

// Amend modifies a file and amends the last commit using raw git
func (s *TestShell) Amend(filename, content string) *TestShell {
	s.t.Helper()
	err := s.scene.Repo.CreateChangeAndAmend(content, filename)
	require.NoError(s.t, err, "failed to amend with %s", filename)
	return s
}

// Modify creates a file change and uses stackit modify to amend (with auto-restack)
func (s *TestShell) Modify(filename, content string) *TestShell {
	s.t.Helper()
	// Create the change (staged)
	err := s.scene.Repo.CreateChange(content, filename, false)
	require.NoError(s.t, err, "failed to write %s", filename)
	// Use stackit modify to amend with auto-restack
	return s.Run("modify -n")
}

// ModifyWithMessage creates a file change and uses stackit modify with a new message
func (s *TestShell) ModifyWithMessage(filename, content, message string) *TestShell {
	s.t.Helper()
	// Create the change (staged)
	err := s.scene.Repo.CreateChange(content, filename, false)
	require.NoError(s.t, err, "failed to write %s", filename)
	// Use stackit modify to amend with message
	return s.Run("modify -m '" + message + "'")
}

// Commit creates a file change and commits it
func (s *TestShell) Commit(filename, message string) *TestShell {
	s.t.Helper()
	err := s.scene.Repo.CreateChangeAndCommit(message, filename)
	require.NoError(s.t, err, "failed to commit %s", filename)
	return s
}

// =============================================================================
// Output Inspection
// =============================================================================

// Output returns the last command's output
func (s *TestShell) Output() string {
	return s.lastOutput
}

// OutputContains asserts the last output contains the given string
func (s *TestShell) OutputContains(substr string) *TestShell {
	s.t.Helper()
	require.Contains(s.t, s.lastOutput, substr)
	return s
}

// OutputNotContains asserts the last output does NOT contain the given string
func (s *TestShell) OutputNotContains(substr string) *TestShell {
	s.t.Helper()
	require.NotContains(s.t, s.lastOutput, substr)
	return s
}

// =============================================================================
// Assertions
// =============================================================================

// GetLatestSnapshotID returns the ID of the most recent undo snapshot
func (s *TestShell) GetLatestSnapshotID() string {
	s.t.Helper()
	undoDir := filepath.Join(s.scene.Dir, ".git", "stackit", "undo")
	entries, err := os.ReadDir(undoDir)
	require.NoError(s.t, err)
	require.NotEmpty(s.t, entries, "no snapshots found")

	var latest string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			if latest == "" || entry.Name() > latest {
				latest = entry.Name()
			}
		}
	}
	return strings.TrimSuffix(latest, ".json")
}

// UndoLatest undos the last operation using the latest snapshot
func (s *TestShell) UndoLatest() *TestShell {
	s.t.Helper()
	id := s.GetLatestSnapshotID()
	return s.Run("undo --snapshot " + id + " --yes")
}

// OnBranch asserts we're on the expected branch
func (s *TestShell) OnBranch(expected string) *TestShell {
	s.t.Helper()
	branch, err := s.scene.Repo.CurrentBranchName()
	require.NoError(s.t, err)
	require.Equal(s.t, expected, branch)
	return s
}

// HasBranches asserts the repo has exactly these branches
func (s *TestShell) HasBranches(branches ...string) *TestShell {
	s.t.Helper()
	cmd := exec.Command("git", "-C", s.scene.Repo.Dir,
		"for-each-ref", "refs/heads/", "--format=%(refname:short)")
	output, err := cmd.Output()
	require.NoError(s.t, err, "Failed to list branches")

	actualBranches := strings.Split(strings.TrimSpace(string(output)), "\n")
	filtered := []string{}
	for _, branch := range actualBranches {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == "prod" || branch == "x2" || s.isWorktreeAnchorBranch(branch) {
			continue
		}
		filtered = append(filtered, branch)
	}

	slices.Sort(filtered)
	slices.Sort(branches)
	require.Equal(s.t, branches, filtered, "Branches do not match")
	return s
}

// CommitCount asserts the number of commits between two refs
func (s *TestShell) CommitCount(from, to string, expected int) *TestShell {
	s.t.Helper()
	cmd := exec.Command("git", "log", "--oneline", from+".."+to)
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	require.NoError(s.t, err)
	actual := countNonEmptyLines(string(output))
	require.Equal(s.t, expected, actual, "expected %d commits between %s..%s, got %d", expected, from, to, actual)
	return s
}

// =============================================================================
// Logging
// =============================================================================

// Log prints a message (useful for documenting test steps)
func (s *TestShell) Log(msg string) *TestShell {
	s.t.Log(msg)
	return s
}

// =============================================================================
// Utility Functions
// =============================================================================

// splitArgs splits a command string into args, respecting quotes
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			switch {
			case inQuote && r == quoteChar:
				inQuote = false
			case !inQuote:
				inQuote = true
				quoteChar = r
			default:
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// countNonEmptyLines counts lines that have non-whitespace content
func countNonEmptyLines(s string) int {
	count := 0
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return canonicalPath(a) == canonicalPath(b)
}

func isMainWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

func parseWorktreePaths(output string) []string {
	var paths []string
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, strings.TrimSpace(after))
		}
	}
	return paths
}

func (s *TestShell) resetOrigin(root string) {
	if root == "" {
		return
	}
	remotePath := s.remotePath()
	if remotePath == "" {
		return
	}

	refs := runGitInDir(s.t, remotePath, "--git-dir", remotePath, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/stackit")
	for _, ref := range splitLines(refs) {
		if ref != "" && ref != "refs/heads/main" {
			_ = runGitInDir(s.t, remotePath, "--git-dir", remotePath, "update-ref", "-d", ref)
		}
	}
	_ = runGitInDir(s.t, remotePath, "--git-dir", remotePath, "update-ref", "refs/heads/main", root)
	_ = runGitInDir(s.t, remotePath, "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")
}

func (s *TestShell) remotePath() string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func runGitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), string(output))
	return strings.TrimSpace(string(output))
}

// =============================================================================
// Stack Fixtures - Common stack patterns for tests
// =============================================================================

// CreateLinearStack3 creates a linear stack via stackit create: main -> a -> b -> c
// This is a convenience wrapper for CreateLinearStack("a", "b", "c").
// Returns the TestShell for method chaining.
func (s *TestShell) CreateLinearStack3() *TestShell {
	return s.CreateLinearStack("a", "b", "c")
}

// CreateLinearStack creates a linear stack with the given branch names using stackit create.
// Each branch is created as a child of the previous one, starting from the current branch.
// Example: CreateLinearStack("a", "b", "c") creates: current -> a -> b -> c
// Returns the TestShell on the last created branch.
func (s *TestShell) CreateLinearStack(names ...string) *TestShell {
	s.t.Helper()
	for _, name := range names {
		s.Write(name+".txt", "content for "+name).
			Run("create " + name + " -m 'Add " + name + "'")
	}
	return s
}

// CreateDiamondStack creates a diamond-shaped stack: main -> parent -> [child1, child2]
// This is useful for testing operations with parallel branches.
// Returns the TestShell on child2.
func (s *TestShell) CreateDiamondStack() *TestShell {
	s.t.Helper()
	// Create parent
	s.Write("parent.txt", "parent content").
		Run("create parent -m 'Add parent'")
	// Create first child
	s.Write("child1.txt", "child1 content").
		Run("create child1 -m 'Add child1'")
	// Go back to parent and create second child
	s.Checkout("parent").
		Write("child2.txt", "child2 content").
		Run("create child2 -m 'Add child2'")
	return s
}

// =============================================================================
// Worktree Helpers
// =============================================================================

// GetWorktreePath returns the path to a worktree for a given worktree name or anchor.
// It first tries refs/stackit/worktrees/{nameOrAnchor}, then scans worktree
// metadata for a matching display name.
func (s *TestShell) GetWorktreePath(nameOrAnchor string) string {
	s.t.Helper()

	if path, ok := s.getWorktreePathByRef(nameOrAnchor); ok {
		return path
	}

	cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", "refs/stackit/worktrees")
	cmd.Dir = s.scene.Dir
	refsOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to list worktree refs")

	for _, refName := range strings.Fields(string(refsOutput)) {
		cmd = exec.Command("git", "show-ref", "-s", refName)
		cmd.Dir = s.scene.Dir
		shaOutput, err := cmd.Output()
		require.NoError(s.t, err, "failed to get worktree ref for %s", refName)

		sha := strings.TrimSpace(string(shaOutput))
		cmd = exec.Command("git", "cat-file", "-p", sha)
		cmd.Dir = s.scene.Dir
		blobOutput, err := cmd.Output()
		require.NoError(s.t, err, "failed to read worktree metadata blob")

		var meta struct {
			Name         string `json:"name"`
			Path         string `json:"path"`
			AnchorBranch string `json:"stackRoot"`
		}
		err = json.Unmarshal(blobOutput, &meta)
		require.NoError(s.t, err, "failed to parse worktree metadata")
		if meta.Name == nameOrAnchor || meta.AnchorBranch == nameOrAnchor {
			require.NotEmpty(s.t, meta.Path, "worktree path is empty for %s", nameOrAnchor)
			return meta.Path
		}
	}

	require.Failf(s.t, "worktree not found", "failed to find worktree metadata for %s", nameOrAnchor)
	return ""
}

func (s *TestShell) getWorktreePathByRef(anchor string) (string, bool) {
	s.t.Helper()
	refName := "refs/stackit/worktrees/" + anchor
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()
	if err != nil {
		return "", false
	}

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to read worktree metadata blob")

	// Parse JSON to extract path
	var meta struct {
		Path string `json:"path"`
	}
	err = json.Unmarshal(blobOutput, &meta)
	require.NoError(s.t, err, "failed to parse worktree metadata")
	require.NotEmpty(s.t, meta.Path, "worktree path is empty for %s", anchor)

	return meta.Path, true
}

// InWorktree creates a new TestShell operating in the specified worktree path.
// This allows running commands in the worktree context.
func (s *TestShell) InWorktree(worktreePath string) *TestShell {
	s.t.Helper()

	// Create a new scene pointing to the worktree directory
	worktreeScene := &testhelpers.Scene{
		Dir:  worktreePath,
		Repo: testhelpers.NewGitRepoFromExisting(s.t, worktreePath),
	}

	return &TestShell{
		t:            s.t,
		scene:        worktreeScene,
		binaryPath:   s.binaryPath,
		inProcessCLI: s.inProcessCLI,
	}
}

// SetPrState sets the PR state in metadata for a branch.
// State can be "OPEN", "MERGED", "CLOSED", etc.
func (s *TestShell) SetPrState(branch, state string) *TestShell {
	s.t.Helper()

	// Read existing metadata
	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()

	var meta map[string]any
	if err == nil {
		// Ref exists, read it
		sha := strings.TrimSpace(string(shaOutput))
		cmd = exec.Command("git", "cat-file", "-p", sha)
		cmd.Dir = s.scene.Dir
		blobOutput, err := cmd.Output()
		require.NoError(s.t, err, "failed to read metadata blob for %s", branch)
		err = json.Unmarshal(blobOutput, &meta)
		require.NoError(s.t, err, "failed to parse metadata for %s", branch)
	} else {
		// No existing metadata
		meta = make(map[string]any)
	}

	// Update PR state
	prInfo, ok := meta["prInfo"].(map[string]any)
	if !ok {
		prInfo = make(map[string]any)
	}
	prInfo["state"] = state
	meta["prInfo"] = prInfo

	// Write back
	jsonData, err := json.Marshal(meta)
	require.NoError(s.t, err, "failed to marshal metadata")

	// Create blob
	cmd = exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = s.scene.Dir
	cmd.Stdin = strings.NewReader(string(jsonData))
	newShaOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to create metadata blob")

	// Update ref
	newSha := strings.TrimSpace(string(newShaOutput))
	cmd = exec.Command("git", "update-ref", refName, newSha)
	cmd.Dir = s.scene.Dir
	err = cmd.Run()
	require.NoError(s.t, err, "failed to update metadata ref")

	return s
}

// ExpectBranchParent asserts a branch has the expected parent in its metadata.
func (s *TestShell) ExpectBranchParent(branch, expectedParent string) *TestShell {
	s.t.Helper()

	meta := s.readBranchMeta(branch)

	require.NotNil(s.t, meta.ParentBranchName, "branch %s has no parent", branch)
	actualParent := s.visibleParent(*meta.ParentBranchName)
	require.Equal(s.t, expectedParent, actualParent,
		"branch %s expected parent %s, got %s", branch, expectedParent, actualParent)

	return s
}

// ExpectStackStructure asserts the entire stack structure matches expected parent-child relationships.
func (s *TestShell) ExpectStackStructure(expected map[string]string) *TestShell {
	s.t.Helper()
	for branch, expectedParent := range expected {
		s.ExpectBranchParent(branch, expectedParent)
	}
	return s
}

type testBranchMeta struct {
	ParentBranchName *string `json:"parentBranchName"`
	BranchType       string  `json:"branchType"`
}

func (s *TestShell) readBranchMeta(branch string) testBranchMeta {
	s.t.Helper()

	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to get metadata ref for %s", branch)

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to read metadata blob for %s", branch)

	var meta testBranchMeta
	err = json.Unmarshal(blobOutput, &meta)
	require.NoError(s.t, err, "failed to parse metadata for %s", branch)
	return meta
}

func (s *TestShell) isWorktreeAnchorBranch(branch string) bool {
	s.t.Helper()

	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()
	if err != nil {
		return false
	}

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	if err != nil {
		return false
	}

	var meta testBranchMeta
	if err := json.Unmarshal(blobOutput, &meta); err != nil {
		return false
	}
	return meta.BranchType == "worktree-anchor"
}

func (s *TestShell) visibleParent(parent string) string {
	s.t.Helper()
	if !s.isWorktreeAnchorBranch(parent) {
		return parent
	}
	meta := s.readBranchMeta(parent)
	if meta.ParentBranchName == nil {
		return parent
	}
	return *meta.ParentBranchName
}

// PRMetadata contains options for setting PR metadata
type PRMetadata struct {
	Number int
	State  string // "OPEN", "MERGED", "CLOSED"
	Draft  bool
	URL    string
}

// ExpectNeedsPRBodyUpdate asserts a branch has (or doesn't have) the NeedsPRBodyUpdate flag set.
func (s *TestShell) ExpectNeedsPRBodyUpdate(branch string, expected bool) *TestShell {
	s.t.Helper()

	// Read local metadata
	refName := "refs/stackit/local-metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()

	if err != nil {
		// No local metadata exists - flag is not set
		if expected {
			s.t.Errorf("branch %s: expected NeedsPRBodyUpdate=true but no local metadata exists", branch)
		}
		return s
	}

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to read local metadata blob for %s", branch)

	var meta struct {
		NeedsPRBodyUpdate bool `json:"needsPRBodyUpdate"`
	}
	err = json.Unmarshal(blobOutput, &meta)
	require.NoError(s.t, err, "failed to parse local metadata for %s", branch)

	require.Equal(s.t, expected, meta.NeedsPRBodyUpdate,
		"branch %s: expected NeedsPRBodyUpdate=%v, got %v", branch, expected, meta.NeedsPRBodyUpdate)

	return s
}

// SetPrMetadata sets full PR metadata for a branch.
func (s *TestShell) SetPrMetadata(branch string, pr PRMetadata) *TestShell {
	s.t.Helper()

	// Read existing metadata
	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()

	var meta map[string]any
	if err == nil {
		// Ref exists, read it
		sha := strings.TrimSpace(string(shaOutput))
		cmd = exec.Command("git", "cat-file", "-p", sha)
		cmd.Dir = s.scene.Dir
		blobOutput, err := cmd.Output()
		require.NoError(s.t, err, "failed to read metadata blob for %s", branch)
		err = json.Unmarshal(blobOutput, &meta)
		require.NoError(s.t, err, "failed to parse metadata for %s", branch)
	} else {
		// No existing metadata
		meta = make(map[string]any)
	}

	// Update PR info
	prInfo := make(map[string]any)
	if pr.Number > 0 {
		prInfo["number"] = pr.Number
	}
	if pr.State != "" {
		prInfo["state"] = pr.State
	}
	if pr.Draft {
		prInfo["draft"] = pr.Draft
	}
	if pr.URL != "" {
		prInfo["url"] = pr.URL
	}
	meta["prInfo"] = prInfo

	// Write back
	jsonData, err := json.Marshal(meta)
	require.NoError(s.t, err, "failed to marshal metadata")

	// Create blob
	cmd = exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = s.scene.Dir
	cmd.Stdin = strings.NewReader(string(jsonData))
	newShaOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to create metadata blob")

	// Update ref
	newSha := strings.TrimSpace(string(newShaOutput))
	cmd = exec.Command("git", "update-ref", refName, newSha)
	cmd.Dir = s.scene.Dir
	err = cmd.Run()
	require.NoError(s.t, err, "failed to update metadata ref")

	return s
}

// GetStackID returns the stack ID for a branch from its metadata.
// Returns empty string if the branch has no stack ID.
func (s *TestShell) GetStackID(branch string) string {
	s.t.Helper()

	// Read metadata
	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()
	if err != nil {
		// No metadata ref exists
		return ""
	}

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	if err != nil {
		return ""
	}

	var meta struct {
		StackID *string `json:"stackId"`
	}
	if err := json.Unmarshal(blobOutput, &meta); err != nil {
		return ""
	}

	if meta.StackID == nil {
		return ""
	}
	return *meta.StackID
}

// ExpectStackID asserts a branch has the expected stack ID.
func (s *TestShell) ExpectStackID(branch, expectedStackID string) *TestShell {
	s.t.Helper()
	actualStackID := s.GetStackID(branch)
	require.Equal(s.t, expectedStackID, actualStackID,
		"branch %s expected stack ID %q, got %q", branch, expectedStackID, actualStackID)
	return s
}

// ExpectStackIDNotEmpty asserts a branch has a non-empty stack ID.
func (s *TestShell) ExpectStackIDNotEmpty(branch string) *TestShell {
	s.t.Helper()
	actualStackID := s.GetStackID(branch)
	require.NotEmpty(s.t, actualStackID, "branch %s expected to have a stack ID, but got empty", branch)
	return s
}

// ExpectStackIDsMatch asserts that all given branches have the same stack ID.
func (s *TestShell) ExpectStackIDsMatch(branches ...string) *TestShell {
	s.t.Helper()
	if len(branches) < 2 {
		return s
	}

	firstStackID := s.GetStackID(branches[0])
	require.NotEmpty(s.t, firstStackID, "branch %s has no stack ID", branches[0])

	for _, branch := range branches[1:] {
		stackID := s.GetStackID(branch)
		require.Equal(s.t, firstStackID, stackID,
			"branch %s has stack ID %q, expected %q (same as %s)",
			branch, stackID, firstStackID, branches[0])
	}
	return s
}

// ExpectStackIDsDiffer asserts that two branches have different stack IDs.
func (s *TestShell) ExpectStackIDsDiffer(branch1, branch2 string) *TestShell {
	s.t.Helper()
	stackID1 := s.GetStackID(branch1)
	stackID2 := s.GetStackID(branch2)
	require.NotEqual(s.t, stackID1, stackID2,
		"branch %s and %s both have stack ID %q, expected different IDs",
		branch1, branch2, stackID1)
	return s
}

// =============================================================================
// Worktree Helpers
// =============================================================================

// AdvanceMain commits a new file on the main branch and pushes it to origin.
// Requires the shell to have a remote (WithRemote).
func AdvanceMain(sh *TestShell, filename string) {
	sh.t.Helper()
	sh.WriteFile(filename, "content from main").
		Git("add " + filename).
		Git("commit -m 'Advance main: " + filename + "'").
		Git("push origin main")
}

// SimulateMerge fast-forward merges the branch into main, pushes, and marks its PR as MERGED.
// Requires the shell to have a remote (WithRemote).
func SimulateMerge(sh *TestShell, branchName string) {
	sh.t.Helper()
	sh.Git("checkout main").
		Git("merge " + branchName + " --ff-only").
		Git("push origin main")
	sh.SetPrState(branchName, "MERGED")
}

// MakeWorktreeDirty writes an uncommitted file into a worktree directory.
func MakeWorktreeDirty(t *testing.T, worktreePath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktreePath, "uncommitted.txt"), []byte("uncommitted"), 0644); err != nil {
		t.Fatalf("failed to make worktree dirty: %v", err)
	}
}

// CreateIndependentStack2 creates main->branch-a->branch-b where each branch
// touches a different file (no file dependencies between branches).
// Used by flatten tests to verify file-independence detection.
func (s *TestShell) CreateIndependentStack2() *TestShell {
	s.t.Helper()
	return s.Write("file-a", "content a").
		Run("create branch-a -m 'Add file A'").
		Write("file-b", "content b").
		Run("create branch-b -m 'Add file B'")
}
