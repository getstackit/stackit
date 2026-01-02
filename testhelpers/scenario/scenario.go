// Package scenario provides a high-level test scenario that combines a Scene,
// an Engine, and a runtime Context to provide a terse API for integration tests.
package scenario

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/create"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/testhelpers"
)

// InProcessRunner is a function that executes a stackit command in-process.
type InProcessRunner func(workDir string, args ...string) (string, error)

var (
	// GlobalInProcessRunner is a registered runner for in-process CLI execution.
	// This is used to break import cycles between scenario and cli packages.
	globalInProcessRunner InProcessRunner
	globalRunnerMu        sync.RWMutex
)

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
	T          *testing.T
	Scene      *testhelpers.Scene
	Engine     engine.Engine
	Context    *app.Context
	BinaryPath string
	InProcess  bool
	Output     *bytes.Buffer
}

// NewScenario creates a new Scenario with an optional setup function.
// It is safe for parallel tests as it uses NewSceneParallel.
func NewScenario(t *testing.T, setup testhelpers.SceneSetup) *Scenario {
	t.Helper()

	// Force non-interactive mode for tests in the current process
	tui.SetInteractive(false)

	scene := testhelpers.NewSceneParallel(t, setup)
	cfg, _ := config.LoadConfig(scene.Dir)
	trunk := cfg.Trunk()
	if trunk == "" {
		trunk = "main"
	}
	maxUndoDepth := cfg.UndoStackDepth()
	if maxUndoDepth <= 0 {
		maxUndoDepth = engine.DefaultMaxUndoStackDepth
	}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          scene.Dir,
		Trunk:             trunk,
		MaxUndoStackDepth: maxUndoDepth,
	})
	require.NoError(t, err)

	output := &bytes.Buffer{}
	ctx := app.NewContextWithOptions(eng, app.GlobalOptions{
		Interactive: false,
		Verify:      true,
		Debug:       os.Getenv("DEBUG") != "",
		Quiet:       true,
	})
	ctx.Splog = tui.NewSplogToWriter(output)
	ctx.RepoRoot = scene.Dir

	return &Scenario{
		T:       t,
		Scene:   scene,
		Engine:  eng,
		Context: ctx,
		Output:  output,
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

// WithInitialCommit creates an initial commit on the main branch.
func (s *Scenario) WithInitialCommit() *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateChangeAndCommit("initial", "init")
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
	if s.Engine.Trunk().GetName() == "main" {
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
				// Create branch without rebuilding engine
				s.CheckoutQuiet(parent)
				s.CreateBranchQuiet(branch)

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
			s.T.Fatal("GlobalInProcessRunner not set. Import stackit.dev/stackit/internal/integration/setup in your test.")
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
	fullArgs := append([]string{"--no-interactive"}, args...)
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
func (s *Scenario) RunCliAndGetOutput(args ...string) (string, error) {
	if s.InProcess {
		runner := GetGlobalInProcessRunner()
		if runner == nil {
			return "", fmt.Errorf("GlobalInProcessRunner not set")
		}
		return runner(s.Scene.Dir, args...)
	}

	if s.BinaryPath == "" {
		return "", fmt.Errorf("BinaryPath not set")
	}
	// Add --no-interactive to all CLI commands in tests
	fullArgs := append([]string{"--no-interactive"}, args...)
	cmd := exec.Command(s.BinaryPath, fullArgs...)
	cmd.Dir = s.Scene.Dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if s.Engine != nil {
		s.Rebuild()
	}
	return string(output), err
}

// RunExpectError executes a stackit CLI command and expects it to fail.
func (s *Scenario) RunExpectError(args ...string) *Scenario {
	s.T.Helper()

	if s.InProcess {
		runner := GetGlobalInProcessRunner()
		if runner == nil {
			s.T.Fatal("GlobalInProcessRunner not set")
		}
		_, err := runner(s.Scene.Dir, args...)
		require.Error(s.T, err, "expected CLI command to fail: stackit %v", args)

		if s.Engine != nil {
			return s.Rebuild()
		}
		return s
	}

	if s.BinaryPath == "" {
		s.T.Fatal("BinaryPath not set")
	}
	// Add --no-interactive to all CLI commands in tests
	fullArgs := append([]string{"--no-interactive"}, args...)
	cmd := exec.Command(s.BinaryPath, fullArgs...)
	cmd.Dir = s.Scene.Dir
	cmd.Env = os.Environ()
	_, err := cmd.CombinedOutput()
	require.Error(s.T, err, "expected CLI command to fail: stackit %v", fullArgs)
	if s.Engine != nil {
		return s.Rebuild()
	}
	return s
}

// Log logs a message using the testing.T object.
func (s *Scenario) Log(args ...interface{}) {
	s.T.Helper()
	s.T.Log(args...)
}

// Logf logs a formatted message using the testing.T object.
func (s *Scenario) Logf(format string, args ...interface{}) {
	s.T.Helper()
	s.T.Logf(format, args...)
}

// Run is an alias for RunCli for backward compatibility in some tests.
func (s *Scenario) Run(args ...string) *Scenario {
	return s.RunCli(args...)
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
// Direct Action API Methods (Option 2: Reduce CLI Binary Spawns)
// These methods call the action functions directly instead of spawning CLI processes.
// =============================================================================

// CreateBranchWithAction creates a new branch using the create action directly.
// This is faster than using RunCli("create ...") because it avoids spawning a process.
// The file content is staged and committed as part of the branch creation.
func (s *Scenario) CreateBranchWithAction(branchName, message string) *Scenario {
	s.T.Helper()
	if s.Engine == nil || s.Context == nil {
		s.T.Fatal("Engine and Context must be initialized. Use NewScenario instead of NewScenarioParallel.")
	}

	// Create create.Options
	createOpts := create.Options{
		BranchName: branchName,
		Message:    message,
	}

	// Get config for branch pattern
	cfg, _ := config.LoadConfig(s.Scene.Dir)
	branchPattern := cfg.GetBranchPattern()
	createOpts.BranchPattern = branchPattern

	// Call create action directly
	err := create.Action(s.Context, createOpts)
	require.NoError(s.T, err, "failed to create branch %s", branchName)

	// Rebuild engine state after action
	return s.Rebuild()
}

// StageChange creates a file change and stages it (for use before Modify or CreateBranchWithAction).
func (s *Scenario) StageChange(filename, content string) *Scenario {
	s.T.Helper()
	err := s.Scene.Repo.CreateChange(content, filename, false) // false = staged
	require.NoError(s.T, err, "failed to stage change in %s", filename)
	return s
}

// Modify performs a modify operation (amend or create commit) using the modify action directly.
// This is faster than using RunCli("modify ...") because it avoids spawning a process.
func (s *Scenario) Modify(opts ...ModifyOption) *Scenario {
	s.T.Helper()
	if s.Engine == nil || s.Context == nil {
		s.T.Fatal("Engine and Context must be initialized. Use NewScenario instead of NewScenarioParallel.")
	}

	modifyOpts := actions.ModifyOptions{
		NoEdit: true, // Default to no-edit for tests
	}

	// Apply any additional options
	for _, opt := range opts {
		opt(&modifyOpts)
	}

	err := actions.ModifyAction(s.Context, modifyOpts)
	require.NoError(s.T, err, "failed to modify branch")

	// Rebuild engine state after action
	return s.Rebuild()
}

// ModifyOption is a function type for configuring modify options
type ModifyOption func(*actions.ModifyOptions)

// ModifyWithMessage sets the commit message for modify
func ModifyWithMessage(message string) ModifyOption {
	return func(opts *actions.ModifyOptions) {
		opts.Message = message
		opts.NoEdit = true
	}
}

// ModifyCreateCommit creates a new commit instead of amending
func ModifyCreateCommit(create bool) ModifyOption {
	return func(opts *actions.ModifyOptions) {
		opts.CreateCommit = create
	}
}

// ModifyWithAll stages all changes before modifying
func ModifyWithAll(all bool) ModifyOption {
	return func(opts *actions.ModifyOptions) {
		opts.All = all
	}
}

// Restack performs a restack operation using the restack action directly.
// This is faster than using RunCli("restack ...") because it avoids spawning a process.
func (s *Scenario) Restack(branchName string, scope engine.StackRange) *Scenario {
	s.T.Helper()
	if s.Engine == nil || s.Context == nil {
		s.T.Fatal("Engine and Context must be initialized. Use NewScenario instead of NewScenarioParallel.")
	}

	restackOpts := actions.RestackOptions{
		BranchName: branchName,
		Scope:      scope,
	}

	err := actions.RestackAction(s.Context, restackOpts)
	require.NoError(s.T, err, "failed to restack branch %s", branchName)

	// Rebuild engine state after action
	return s.Rebuild()
}

// RestackUpstack restacks all upstack branches from the current branch.
func (s *Scenario) RestackUpstack() *Scenario {
	s.T.Helper()
	currentBranch := s.Engine.CurrentBranch()
	if currentBranch == nil {
		s.T.Fatal("not on a branch")
	}
	return s.Restack(currentBranch.GetName(), engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	})
}

// RestackDownstack restacks all downstack branches from the current branch.
func (s *Scenario) RestackDownstack() *Scenario {
	s.T.Helper()
	currentBranch := s.Engine.CurrentBranch()
	if currentBranch == nil {
		s.T.Fatal("not on a branch")
	}
	return s.Restack(currentBranch.GetName(), engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    false,
		RecursiveChildren: false,
	})
}

// RestackOnly restacks only the current branch.
func (s *Scenario) RestackOnly() *Scenario {
	s.T.Helper()
	currentBranch := s.Engine.CurrentBranch()
	if currentBranch == nil {
		s.T.Fatal("not on a branch")
	}
	return s.Restack(currentBranch.GetName(), engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    true,
		RecursiveChildren: false,
	})
}

// Squash performs a squash operation using the squash action directly.
// This is faster than using RunCli("squash ...") because it avoids spawning a process.
func (s *Scenario) Squash(opts ...SquashOption) *Scenario {
	s.T.Helper()
	if s.Engine == nil || s.Context == nil {
		s.T.Fatal("Engine and Context must be initialized. Use NewScenario instead of NewScenarioParallel.")
	}

	squashOpts := actions.SquashOptions{
		NoEdit: true, // Default to no-edit for tests
	}

	// Apply any additional options
	for _, opt := range opts {
		opt(&squashOpts)
	}

	err := actions.SquashAction(s.Context, squashOpts)
	require.NoError(s.T, err, "failed to squash branch")

	// Rebuild engine state after action
	return s.Rebuild()
}

// SquashOption is a function type for configuring squash options
type SquashOption func(*actions.SquashOptions)

// SquashWithMessage sets the commit message for squash
func SquashWithMessage(message string) SquashOption {
	return func(opts *actions.SquashOptions) {
		opts.Message = message
		opts.NoEdit = true
	}
}
