package testhelpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

var (
	minimalTemplateDir  string
	minimalTemplateErr  error
	minimalTemplateOnce sync.Once
	minimalTemplateMu   sync.RWMutex

	basicTemplateDir  string
	basicTemplateErr  error
	basicTemplateOnce sync.Once
	basicTemplateMu   sync.RWMutex

	initialCommitTemplateDir  string
	initialCommitTemplateErr  error
	initialCommitTemplateOnce sync.Once
	initialCommitTemplateMu   sync.RWMutex

	remoteRepoTemplateDir string
	remoteBareTemplateDir string
	remoteTemplateErr     error
	remoteTemplateOnce    sync.Once
	remoteTemplateMu      sync.RWMutex
)

func getMinimalTemplate(t *testing.T) string {
	minimalTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "stackit-test-minimal-template-*")
		if err != nil {
			minimalTemplateMu.Lock()
			minimalTemplateErr = fmt.Errorf("failed to create minimal template dir: %w", err)
			minimalTemplateMu.Unlock()
			return
		}

		// Initialize the minimal repo
		_, err = NewGitRepo(dir)
		if err != nil {
			minimalTemplateMu.Lock()
			minimalTemplateErr = fmt.Errorf("failed to init minimal template repo: %w", err)
			minimalTemplateMu.Unlock()
			return
		}

		// Pre-bake stackit config files into the template
		// This avoids writing them for every test repo clone
		if err := writeStackitConfigs(dir); err != nil {
			minimalTemplateMu.Lock()
			minimalTemplateErr = fmt.Errorf("failed to write config files to template: %w", err)
			minimalTemplateMu.Unlock()
			return
		}

		minimalTemplateMu.Lock()
		minimalTemplateDir = dir
		minimalTemplateMu.Unlock()
	})

	minimalTemplateMu.RLock()
	err := minimalTemplateErr
	dir := minimalTemplateDir
	minimalTemplateMu.RUnlock()

	if err != nil {
		t.Fatalf("Minimal template initialization failed: %v", err)
	}

	return dir
}

func getBasicTemplate(t *testing.T) string {
	basicTemplateOnce.Do(func() {
		minimalDir := getMinimalTemplate(t)

		dir, err := os.MkdirTemp("", "stackit-test-basic-template-*")
		if err != nil {
			basicTemplateMu.Lock()
			basicTemplateErr = fmt.Errorf("failed to create basic template dir: %w", err)
			basicTemplateMu.Unlock()
			return
		}

		// Clone from minimal
		repo, err := NewGitRepoFromTemplate(dir, minimalDir)
		if err != nil {
			basicTemplateMu.Lock()
			basicTemplateErr = fmt.Errorf("failed to init basic template repo: %w", err)
			basicTemplateMu.Unlock()
			return
		}

		// Apply BasicSceneSetup
		if err := BasicSceneSetup(&Scene{Repo: repo, Dir: dir}); err != nil {
			basicTemplateMu.Lock()
			basicTemplateErr = fmt.Errorf("failed to run basic setup on template: %w", err)
			basicTemplateMu.Unlock()
			return
		}

		basicTemplateMu.Lock()
		basicTemplateDir = dir
		basicTemplateMu.Unlock()
	})

	basicTemplateMu.RLock()
	err := basicTemplateErr
	dir := basicTemplateDir
	basicTemplateMu.RUnlock()

	if err != nil {
		t.Fatalf("Basic template initialization failed: %v", err)
	}

	return dir
}

func getInitialCommitTemplate(t *testing.T) string {
	initialCommitTemplateOnce.Do(func() {
		minimalDir := getMinimalTemplate(t)

		dir, err := os.MkdirTemp("", "stackit-test-initial-template-*")
		if err != nil {
			initialCommitTemplateMu.Lock()
			initialCommitTemplateErr = fmt.Errorf("failed to create initial commit template dir: %w", err)
			initialCommitTemplateMu.Unlock()
			return
		}

		repo, err := NewGitRepoFromTemplate(dir, minimalDir)
		if err != nil {
			initialCommitTemplateMu.Lock()
			initialCommitTemplateErr = fmt.Errorf("failed to init initial commit template repo: %w", err)
			initialCommitTemplateMu.Unlock()
			return
		}

		if err := InitialCommitSceneSetup(&Scene{Repo: repo, Dir: dir}); err != nil {
			initialCommitTemplateMu.Lock()
			initialCommitTemplateErr = fmt.Errorf("failed to run initial commit setup on template: %w", err)
			initialCommitTemplateMu.Unlock()
			return
		}

		initialCommitTemplateMu.Lock()
		initialCommitTemplateDir = dir
		initialCommitTemplateMu.Unlock()
	})

	initialCommitTemplateMu.RLock()
	err := initialCommitTemplateErr
	dir := initialCommitTemplateDir
	initialCommitTemplateMu.RUnlock()

	if err != nil {
		t.Fatalf("Initial commit template initialization failed: %v", err)
	}

	return dir
}

func getRemoteTemplates(t *testing.T) (string, string) {
	remoteTemplateOnce.Do(func() {
		initialDir := getInitialCommitTemplate(t)

		bundleDir, err := os.MkdirTemp("", "stackit-test-remote-template-*")
		if err != nil {
			remoteTemplateMu.Lock()
			remoteTemplateErr = fmt.Errorf("failed to create remote template dir: %w", err)
			remoteTemplateMu.Unlock()
			return
		}

		repoDir := filepath.Join(bundleDir, "repo")
		bareDir := filepath.Join(bundleDir, "origin.git")

		repo, err := NewGitRepoFromTemplate(repoDir, initialDir)
		if err != nil {
			remoteTemplateMu.Lock()
			remoteTemplateErr = fmt.Errorf("failed to init remote template repo: %w", err)
			remoteTemplateMu.Unlock()
			return
		}

		cmd := exec.Command("git", "init", "--bare", bareDir)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if err := cmd.Run(); err != nil {
			remoteTemplateMu.Lock()
			remoteTemplateErr = fmt.Errorf("failed to init remote template bare repo: %w", err)
			remoteTemplateMu.Unlock()
			return
		}

		if err := repo.RunGitCommand("remote", "add", "origin", bareDir); err != nil {
			remoteTemplateMu.Lock()
			remoteTemplateErr = fmt.Errorf("failed to add remote to template repo: %w", err)
			remoteTemplateMu.Unlock()
			return
		}
		if err := repo.RunGitCommand("push", "-u", "origin", "main"); err != nil {
			remoteTemplateMu.Lock()
			remoteTemplateErr = fmt.Errorf("failed to push template repo to origin: %w", err)
			remoteTemplateMu.Unlock()
			return
		}

		remoteTemplateMu.Lock()
		remoteRepoTemplateDir = repoDir
		remoteBareTemplateDir = bareDir
		remoteTemplateMu.Unlock()
	})

	remoteTemplateMu.RLock()
	err := remoteTemplateErr
	repoDir := remoteRepoTemplateDir
	bareDir := remoteBareTemplateDir
	remoteTemplateMu.RUnlock()

	if err != nil {
		t.Fatalf("Remote template initialization failed: %v", err)
	}

	return repoDir, bareDir
}

func setupMatches(setup SceneSetup, target SceneSetup) bool {
	if setup == nil || target == nil {
		return setup == nil && target == nil
	}
	return reflect.ValueOf(setup).Pointer() == reflect.ValueOf(target).Pointer()
}

func templateForSetup(t *testing.T, setup SceneSetup) (string, bool) {
	switch {
	case setupMatches(setup, BasicSceneSetup):
		return getBasicTemplate(t), true
	case setupMatches(setup, InitialCommitSceneSetup):
		return getInitialCommitTemplate(t), true
	default:
		return getMinimalTemplate(t), false
	}
}

// Scene represents a test scene with a temporary directory and Git repository.
// This is the Go equivalent of the TypeScript AbstractScene.
type Scene struct {
	Dir    string
	Repo   *GitRepo
	oldDir string
}

// SceneSetup is a function type for setting up a scene.
type SceneSetup func(*Scene) error

// NewScene creates a new test scene with a temporary directory and Git repository.
// It automatically handles cleanup using t.Cleanup().
// NOTE: This function uses os.Chdir() and is NOT safe for parallel tests.
// Use NewSceneParallel for tests that can run in parallel.
func NewScene(t *testing.T, setup SceneSetup) *Scene {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "stackit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Save current directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	templateDir, skipSetup := templateForSetup(t, setup)
	repo, err := NewGitRepoFromTemplate(tmpDir, templateDir)

	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create Git repo (template: %s, target: %s): %v", templateDir, tmpDir, err)
	}

	scene := &Scene{
		Dir:    tmpDir,
		Repo:   repo,
		oldDir: oldDir,
	}

	// Change to temp directory
	if err := os.Chdir(tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Run custom setup if provided and not already covered by a cached template.
	if setup != nil && !skipSetup {
		if err := setup(scene); err != nil {
			_ = os.Chdir(oldDir)
			_ = os.RemoveAll(tmpDir)
			t.Fatalf("Setup failed: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		_ = os.Chdir(oldDir)
		if os.Getenv("DEBUG") == "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	return scene
}

// NewSceneParallel creates a new test scene that is safe for parallel tests.
// Unlike NewScene, this does NOT change the working directory.
// Tests using this must ensure all git operations use explicit directory paths
// (e.g., via scene.Repo methods or cmd.Dir = scene.Dir).
func NewSceneParallel(t *testing.T, setup SceneSetup) *Scene {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "stackit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	templateDir, skipSetup := templateForSetup(t, setup)
	repo, err := NewGitRepoFromTemplate(tmpDir, templateDir)

	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create Git repo (template: %s, target: %s): %v", templateDir, tmpDir, err)
	}

	scene := &Scene{
		Dir:  tmpDir,
		Repo: repo,
	}

	// Run custom setup if provided and not already covered by a cached template.
	if setup != nil && !skipSetup {
		if err := setup(scene); err != nil {
			_ = os.RemoveAll(tmpDir)
			t.Fatalf("Setup failed: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		if os.Getenv("DEBUG") == "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	return scene
}

// NewRemoteSceneParallel creates a parallel-safe scene with an initial commit,
// a local bare "origin" remote, and main pushed to that remote.
func NewRemoteSceneParallel(t *testing.T) *Scene {
	t.Helper()

	repoTemplateDir, bareTemplateDir := getRemoteTemplates(t)

	tmpDir, err := os.MkdirTemp("", "stackit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	repo, err := NewGitRepoFromTemplate(tmpDir, repoTemplateDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create remote scene repo (template: %s, target: %s): %v", repoTemplateDir, tmpDir, err)
	}

	remoteRoot := t.TempDir()
	remoteDir := filepath.Join(remoteRoot, "origin.git")
	if err := copyDir(bareTemplateDir, remoteDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create remote scene origin (template: %s, target: %s): %v", bareTemplateDir, remoteDir, err)
	}

	if err := repo.RunGitCommand("remote", "set-url", "origin", remoteDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to point scene repo at copied origin: %v", err)
	}

	scene := &Scene{
		Dir:  tmpDir,
		Repo: repo,
	}

	t.Cleanup(func() {
		if os.Getenv("DEBUG") == "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	return scene
}

// writeStackitConfigs writes the default Stackit configuration files to a directory.
// This is used both for templates and for non-template repos.
func writeStackitConfigs(dir string) error {
	// Mark the repo initialized in git config, the modern storage location.
	//
	// This used to pre-bake a legacy .stackit_config JSON file and rely on the
	// first LoadConfig migrating it into git config. That made every scene look
	// like an un-migrated legacy repo, so needsMigration got past its os.Stat
	// short-circuit and spawned `git config --get stackit.trunk` on EVERY
	// LoadConfig call, in every test — and the migration never completed,
	// because it writes the same key it checks. Tests that need a genuinely
	// uninitialized repo call MakeUninitialized.
	if err := MarkInitialized(dir); err != nil {
		return err
	}

	// Write user config (JSON format)
	userConfigPath := filepath.Join(dir, ".git", ".stackit_user_config")
	userConfig := `{
  "tips": false
}
`
	if err := os.WriteFile(userConfigPath, []byte(userConfig), 0600); err != nil {
		return err
	}

	return nil
}

// MarkInitialized sets the trunk key that marks a repo as having run
// `stackit init`, without paying for the command. Scenes start uninitialized;
// harnesses whose tests assume an initialized repo call this once at setup.
func MarkInitialized(dir string) error {
	cmd := exec.Command("git", "config", "--local", "stackit.trunk", "main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set stackit.trunk in %s: %w", dir, err)
	}
	return nil
}

// MakeUninitialized returns a scene directory to the state of a repo that has
// never run `stackit init`, by clearing the trunk key that marks it
// initialized. Use it in tests that exercise init or legacy-JSON migration —
// both branch on stackit.trunk being unset.
func MakeUninitialized(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--local", "--unset", "stackit.trunk")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	// Exit code 5 means the key was already absent, which is the desired state.
	if err := cmd.Run(); err != nil && cmd.ProcessState.ExitCode() != 5 {
		t.Fatalf("failed to unset stackit.trunk in %s: %v", dir, err)
	}
}

// BasicSceneSetup is a setup function that creates a basic scene with a single commit.
func BasicSceneSetup(scene *Scene) error {
	return scene.Repo.CreateChangeAndCommit("1", "1")
}

// InitialCommitSceneSetup creates a repo with a conventional "initial" commit.
func InitialCommitSceneSetup(scene *Scene) error {
	return scene.Repo.CreateChangeAndCommit("initial", "init")
}
