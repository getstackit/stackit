// Package scenario provides a high-level test scenario that combines a Scene,
// an Engine, and a runtime Context to provide a terse API for integration tests.
package scenario

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/testhelpers"
)

// InProcessRunner is a function that executes a stackit command in-process.
type InProcessRunner func(workDir string, args ...string) (string, error)

var (
	// GlobalInProcessRunner is a registered runner for in-process CLI execution.
	// This is used to break import cycles between scenario and cli packages.
	globalInProcessRunner InProcessRunner
	globalRunnerMu        sync.RWMutex
)

// flagNoInteractive is the CLI flag used to suppress interactive prompts in tests.
const flagNoInteractive = "--no-interactive"

// defaultTrunk is the trunk branch name used by the test scenarios.
const defaultTrunk = "main"

// SetGlobalInProcessRunner sets the global in-process runner in a thread-safe way.
func SetGlobalInProcessRunner(runner InProcessRunner) {
	globalRunnerMu.Lock()
	defer globalRunnerMu.Unlock()
	globalInProcessRunner = runner
}

// GetGlobalInProcessRunner gets the global in-process runner in a thread-safe way.
func GetGlobalInProcessRunner() InProcessRunner {
	globalRunnerMu.RLock()
	defer globalRunnerMu.RUnlock()
	return globalInProcessRunner
}

// GlobalInProcessRunner is kept for backward compatibility.
// It's a pointer to a struct that provides thread-safe access.
// Use SetGlobalInProcessRunner and GetGlobalInProcessRunner directly for better clarity.
var GlobalInProcessRunner = &struct {
	Get func() InProcessRunner
	Set func(InProcessRunner)
}{
	Get: GetGlobalInProcessRunner,
	Set: SetGlobalInProcessRunner,
}

// Scenario represents a high-level test scenario that combines a Scene,
// an Engine, and a runtime Context to provide a terse API for integration tests.
type Scenario struct {
	T      *testing.T
	Scene  *testhelpers.Scene
	Engine engine.Engine
	// Git is the runner the engine was built on, for tests that need to
	// observe or stage raw repository state below the engine abstraction.
	Git git.Runner
	// Metadata is the engine's metadata store, for tests that seed or inspect
	// raw metadata refs.
	Metadata   *git.MetadataStore
	Context    *app.Context
	BinaryPath string
	InProcess  bool
	Output     *bytes.Buffer
}

// NewScenario creates a new Scenario with an optional setup function.
// It is safe for parallel tests as it uses NewSceneParallel.
func NewScenario(t *testing.T, setup testhelpers.SceneSetup) *Scenario {
	t.Helper()
	return newScenarioWithScene(t, testhelpers.NewSceneParallel(t, setup))
}

// NewRemoteScenario creates a new Scenario backed by a cached scene that
// already has a local bare "origin" remote and trunk pushed there.
func NewRemoteScenario(t *testing.T) *Scenario {
	t.Helper()
	return newScenarioWithScene(t, testhelpers.NewRemoteSceneParallel(t))
}

func newScenarioWithScene(t *testing.T, scene *testhelpers.Scene) *Scenario {
	t.Helper()

	// Force non-interactive mode for tests in the current process
	tui.SetInteractive(false)

	cfg, cfgErr := config.LoadConfig(scene.Dir)
	trunk := cfg.Trunk()
	if trunk == "" {
		trunk = defaultTrunk
	}
	maxUndoDepth := cfg.UndoStackDepth()
	if maxUndoDepth <= 0 {
		maxUndoDepth = engine.DefaultMaxUndoStackDepth
	}
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	metadata := git.NewMetadataStore(runner)
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          scene.Dir,
		Trunk:             trunk,
		MaxUndoStackDepth: maxUndoDepth,
		Git:               runner,
		Metadata:          metadata,
	})
	require.NoError(t, err)

	buf := &bytes.Buffer{}
	ctx := app.NewContext(eng,
		app.WithRepoRoot(scene.Dir),
		app.WithWriter(buf),
		app.WithGitHubRunner(runner),
		app.WithGlobalOptions(app.GlobalOptions{
			Interactive: false,
			Verify:      true,
			Debug:       os.Getenv("DEBUG") != "",
			Quiet:       false,
		}),
	)

	// Mirror bootstrap: actions read resolved config from the context rather
	// than loading it from disk themselves.
	if cfgErr == nil {
		ctx.Config = cfg
	}

	return &Scenario{
		T:        t,
		Scene:    scene,
		Engine:   eng,
		Git:      runner,
		Metadata: metadata,
		Context:  ctx,
		Output:   buf,
	}
}

// NewScenarioParallel creates a new Scenario that is safe for parallel tests.
// It does NOT set global environment variables or initialize the Go Engine/Context.
// Use this for tests that primarily call the CLI binary.
func NewScenarioParallel(t *testing.T, setup testhelpers.SceneSetup) *Scenario {
	t.Helper()
	scene := testhelpers.NewSceneParallel(t, setup)
	return &Scenario{
		T:     t,
		Scene: scene,
	}
}

// EnsureStackID returns the stack ID for branch, creating stack metadata if
// the stack has none yet. EnsureStackID is an engine implementation detail kept
// off the Engine interface, so tests reach it through the concrete engine.
func (s *Scenario) EnsureStackID(branch string) string {
	s.T.Helper()
	impl, ok := s.Engine.(interface {
		EnsureStackID(ctx context.Context, branch engine.Branch) (string, error)
	})
	require.True(s.T, ok, "engine does not implement EnsureStackID")
	id, err := impl.EnsureStackID(context.Background(), s.Engine.GetBranch(branch))
	require.NoError(s.T, err)
	return id
}

// WithInitialCommit creates an initial commit on the main branch.
func (s *Scenario) WithInitialCommit() *Scenario {
	s.T.Helper()
	err := testhelpers.InitialCommitSceneSetup(s.Scene)
	require.NoError(s.T, err)
	return s
}

// WithUncommittedChange creates an uncommitted change in the repository.
func (s *Scenario) WithUncommittedChange(name string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateChange("unstaged content", name, true)
	require.NoError(s.T, err)
	return s
}

// RunGit runs a git command in the scenario's repository.
func (s *Scenario) RunGit(args ...string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.RunGitCommand(args...)
	require.NoError(s.T, err)
	return s
}

// Checkout checks out a branch and rebuilds the engine.
func (s *Scenario) Checkout(branch string) *Scenario {
	s.T.Helper()
	return s.CheckoutQuiet(branch).Rebuild()
}

// CheckoutQuiet checks out a branch without rebuilding the engine.
func (s *Scenario) CheckoutQuiet(branch string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CheckoutBranch(branch)
	require.NoError(s.T, err)
	return s
}

// CreateBranch creates and checks out a new branch and rebuilds the engine.
func (s *Scenario) CreateBranch(name string) *Scenario {
	s.T.Helper()
	return s.CreateBranchQuiet(name).Rebuild()
}

// CreateBranchQuiet creates and checks out a new branch without rebuilding the engine.
func (s *Scenario) CreateBranchQuiet(name string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateAndCheckoutBranch(name)
	require.NoError(s.T, err)
	return s
}

// CreateBranchFromQuiet creates and checks out a new branch from an explicit
// start point without rebuilding the engine, in a single git invocation.
func (s *Scenario) CreateBranchFromQuiet(name, startPoint string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateAndCheckoutBranchFrom(name, startPoint)
	require.NoError(s.T, err)
	return s
}

// Rebuild refreshes the engine's internal state from the Git repository.
func (s *Scenario) Rebuild() *Scenario {
	s.T.Helper()
	if s.Engine != nil {
		err := s.Engine.Rebuild(s.Engine.Trunk().GetName())
		require.NoError(s.T, err)
	}
	return s
}

// Commit creates an empty commit with the given message.
func (s *Scenario) Commit(message string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.RunGitCommand("commit", "--allow-empty", "-m", message)
	require.NoError(s.T, err)
	return s
}

// CommitChange creates a file change and commits it.
func (s *Scenario) CommitChange(name, message string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateChangeAndCommit(message, name)
	require.NoError(s.T, err)
	return s
}

// SyncRemoteTrunkToLocal advances the local remote-tracking ref
// (refs/remotes/origin/<trunk>) to the current local trunk tip. It models a
// `git fetch` after the trunk's new commits have already landed on the remote —
// the "synced trunk" state — without running a full push/fetch round-trip.
//
// Use it after advancing local trunk (e.g. simulating a merge by committing on
// main) so the restack trunk guard sees origin/<trunk> == local trunk. Without
// it, local trunk looks ahead of the remote, which is the distinct "un-pushed
// local commits" state the guard is meant to reject.
func (s *Scenario) SyncRemoteTrunkToLocal() *Scenario {
	s.T.Helper()
	trunk := defaultTrunk
	if s.Engine != nil {
		trunk = s.Engine.Trunk().GetName()
	}
	sha, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", trunk)
	require.NoError(s.T, err)
	err = s.Scene.Repo.RunGitCommand("update-ref", "refs/remotes/origin/"+trunk, strings.TrimSpace(sha))
	require.NoError(s.T, err)
	return s
}

// TrackBranch tracks a branch with a parent in the engine.
func (s *Scenario) TrackBranch(branch, parent string) *Scenario {
	s.T.Helper()
	err := s.Engine.TrackBranch(context.Background(), branch, parent)
	require.NoError(s.T, err)
	return s
}

// WithStack sets up a branch hierarchy. The map keys are branch names,
// and values are their parent branch names.
// It automatically creates a commit on each branch and tracks it.
func (s *Scenario) WithStack(structure map[string]string) *Scenario {
	s.T.Helper()

	// Ensure we have an initial commit on main if it's the root
	if s.Engine.Trunk().GetName() == defaultTrunk {
		messages, _ := s.Scene.Repo.ListCurrentBranchCommitMessages()
		if len(messages) == 0 {
			s.WithInitialCommit()
		}
	}

	// We need to create branches in topological order (parents before children).
	// For simplicity in tests, we'll just keep trying until all are created
	// or we stop making progress.
	created := make(map[string]bool)
	created[s.Engine.Trunk().GetName()] = true

	for len(created) < len(structure)+1 {
		progress := false
		for branch, parent := range structure {
			if created[branch] {
				continue
			}
			if created[parent] {
				// Create branch from its parent in a single git invocation
				// (avoids a separate checkout of the parent first).
				s.CreateBranchFromQuiet(branch, parent)

				err := s.Scene.Repo.CreateChangeAndCommit("change on "+branch, branch)
				require.NoError(s.T, err)

				// Track it
				s.TrackBranch(branch, parent)

				created[branch] = true
				progress = true
			}
		}
		if !progress {
			s.T.Fatalf("could not resolve stack structure: circular dependency or missing parent")
		}
	}

	// Rebuild once at the end to ensure engine state is fully consistent
	return s.Rebuild()
}

// ExpectStackStructure asserts that the engine's parent-child relationships match the expected map.
func (s *Scenario) ExpectStackStructure(expected map[string]string) *Scenario {
	s.T.Helper()
	for branch, expectedParent := range expected {
		branchObj := s.Engine.GetBranch(branch)
		actualParent := branchObj.GetParent()
		if actualParent == nil {
			s.T.Errorf("Parent of %s is nil, expected %s", branch, expectedParent)
			continue
		}
		require.Equal(s.T, expectedParent, actualParent.GetName(), "Parent of %s does not match", branch)
	}
	return s
}

// BranchCommitCount returns the number of commits a branch carries relative to
// its divergence base, resolved via the batched stats reader (BatchBranchStats).
// It replaces the removed per-branch Engine.GetCommitCount accessor and uses the
// same base resolution, so counts are unchanged.
func (s *Scenario) BranchCommitCount(branch string) int {
	s.T.Helper()
	b := s.Engine.GetBranch(branch)
	return s.Engine.BatchBranchStats(engine.BranchesOf(b))[branch].CommitCount
}

// ExpectBranchFixed asserts that a branch is considered "fixed" (no restack needed) by the engine.
func (s *Scenario) ExpectBranchFixed(branch string) *Scenario {
	s.T.Helper()
	require.True(s.T, s.Engine.GetBranch(branch).IsBranchUpToDate(), "Branch %s should be up to date", branch)
	return s
}

// ExpectBranchNotFixed asserts that a branch is NOT considered "fixed" by the engine.
func (s *Scenario) ExpectBranchNotFixed(branch string) *Scenario {
	s.T.Helper()
	require.False(s.T, s.Engine.GetBranch(branch).IsBranchUpToDate(), "Branch %s should NOT be up to date", branch)
	return s
}

// WithBinaryPath sets the path to the stackit binary for RunCli methods.
func (s *Scenario) WithBinaryPath(path string) *Scenario {
	s.BinaryPath = path
	return s
}

// WithInProcess sets whether to use in-process CLI execution for RunCli methods.
func (s *Scenario) WithInProcess(inProcess bool) *Scenario {
	s.InProcess = inProcess
	return s
}

// RunCli executes a stackit CLI command and rebuilds the engine if it exists.
func (s *Scenario) RunCli(args ...string) *Scenario {
	s.T.Helper()

	if s.InProcess {
		runner := GetGlobalInProcessRunner()
		if runner == nil {
			s.T.Fatal("GlobalInProcessRunner not set. Import github.com/getstackit/stackit/internal/integration/setup in your test.")
		}
		// Use in-process runner if enabled
		_, err := runner(s.Scene.Dir, args...)
		require.NoError(s.T, err, "CLI command failed: stackit %v", args)

		if s.Engine != nil {
			return s.Rebuild()
		}
		return s
	}

	if s.BinaryPath == "" {
		s.T.Fatal("BinaryPath not set. Call WithBinaryPath or WithInProcess(true) first.")
	}
	// Add --no-interactive to all CLI commands in tests
	fullArgs := append([]string{flagNoInteractive}, args...)
	cmd := exec.Command(s.BinaryPath, fullArgs...)
	cmd.Dir = s.Scene.Dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	require.NoError(s.T, err, "CLI command failed: stackit %v\nOutput: %s", fullArgs, string(output))
	if s.Engine != nil {
		return s.Rebuild()
	}
	return s
}

// RunCliAndGetOutput executes a stackit CLI command and returns its output.
// ANSI escape codes are stripped from the output for stable test assertions,
// since lipgloss v2 always generates ANSI codes regardless of terminal type.
func (s *Scenario) RunCliAndGetOutput(args ...string) (string, error) {
	if s.InProcess {
		runner := GetGlobalInProcessRunner()
		if runner == nil {
			return "", fmt.Errorf("GlobalInProcessRunner not set")
		}
		out, err := runner(s.Scene.Dir, args...)
		return ansi.Strip(out), err
	}

	if s.BinaryPath == "" {
		return "", fmt.Errorf("BinaryPath not set")
	}
	// Add --no-interactive to all CLI commands in tests
	fullArgs := append([]string{flagNoInteractive}, args...)
	cmd := exec.Command(s.BinaryPath, fullArgs...)
	cmd.Dir = s.Scene.Dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if s.Engine != nil {
		s.Rebuild()
	}
	return ansi.Strip(string(output)), err
}

// ExpectBranch asserts that the current branch is as expected.
func (s *Scenario) ExpectBranch(expected string) *Scenario {
	s.T.Helper()
	actual, err := s.Scene.Repo.CurrentBranchName()
	require.NoError(s.T, err)
	require.Equal(s.T, expected, actual)
	return s
}

// =============================================================================
// Stack Fixtures - Common stack patterns for tests
// =============================================================================

// WithLinearStack3 creates a linear stack: main -> a -> b -> c
// This is a convenience wrapper for WithLinearStack("a", "b", "c").
func (s *Scenario) WithLinearStack3() *Scenario {
	return s.WithLinearStack("a", "b", "c")
}

// WithLinearStack creates a linear stack with the given branch names.
// Each branch is created as a child of the previous one, starting from trunk.
// Example: WithLinearStack("a", "b", "c") creates: main -> a -> b -> c
func (s *Scenario) WithLinearStack(names ...string) *Scenario {
	s.T.Helper()
	if len(names) == 0 {
		return s
	}

	// Ensure we have an initial commit on trunk
	trunk := s.Engine.Trunk().GetName()
	messages, _ := s.Scene.Repo.ListCurrentBranchCommitMessages()
	if len(messages) == 0 {
		s.WithInitialCommit()
	}

	// Build the stack structure
	structure := make(map[string]string)
	parent := trunk
	for _, name := range names {
		structure[name] = parent
		parent = name
	}

	return s.WithStack(structure)
}
