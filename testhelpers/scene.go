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

// Template repos are built once per test process and cloned by each scene.
var (
	minimalTemplate = sync.OnceValues(func() (string, error) {
		dir, err := os.MkdirTemp("", "stackit-test-minimal-template-*")
		if err != nil {
			return "", fmt.Errorf("failed to create minimal template dir: %w", err)
		}

		// Initialize the minimal repo
		if _, err := NewGitRepo(dir); err != nil {
			return "", fmt.Errorf("failed to init minimal template repo: %w", err)
		}

		// Pre-bake stackit config files into the template
		// This avoids writing them for every test repo clone
		if err := writeStackitConfigs(dir); err != nil {
			return "", fmt.Errorf("failed to write config files to template: %w", err)
		}

		return dir, nil
	})

	basicTemplate = sync.OnceValues(func() (string, error) {
		minimalDir, err := minimalTemplate()
		if err != nil {
			return "", err
		}

		dir, err := os.MkdirTemp("", "stackit-test-basic-template-*")
		if err != nil {
			return "", fmt.Errorf("failed to create basic template dir: %w", err)
		}

		// Clone from minimal
		repo, err := NewGitRepoFromTemplate(dir, minimalDir)
		if err != nil {
			return "", fmt.Errorf("failed to init basic template repo: %w", err)
		}

		// Apply BasicSceneSetup
		if err := BasicSceneSetup(&Scene{Repo: repo, Dir: dir}); err != nil {
			return "", fmt.Errorf("failed to run basic setup on template: %w", err)
		}

		return dir, nil
	})

	initialCommitTemplate = sync.OnceValues(func() (string, error) {
		minimalDir, err := minimalTemplate()
		if err != nil {
			return "", err
		}

		dir, err := os.MkdirTemp("", "stackit-test-initial-template-*")
		if err != nil {
			return "", fmt.Errorf("failed to create initial commit template dir: %w", err)
		}

		repo, err := NewGitRepoFromTemplate(dir, minimalDir)
		if err != nil {
			return "", fmt.Errorf("failed to init initial commit template repo: %w", err)
		}

		if err := InitialCommitSceneSetup(&Scene{Repo: repo, Dir: dir}); err != nil {
			return "", fmt.Errorf("failed to run initial commit setup on template: %w", err)
		}

		return dir, nil
	})

	remoteTemplates = sync.OnceValues(func() (remoteTemplateDirs, error) {
		initialDir, err := initialCommitTemplate()
		if err != nil {
			return remoteTemplateDirs{}, err
		}

		bundleDir, err := os.MkdirTemp("", "stackit-test-remote-template-*")
		if err != nil {
			return remoteTemplateDirs{}, fmt.Errorf("failed to create remote template dir: %w", err)
		}

		repoDir := filepath.Join(bundleDir, "repo")
		bareDir := filepath.Join(bundleDir, "origin.git")

		repo, err := NewGitRepoFromTemplate(repoDir, initialDir)
		if err != nil {
			return remoteTemplateDirs{}, fmt.Errorf("failed to init remote template repo: %w", err)
		}

		cmd := exec.Command("git", "init", "--bare", bareDir)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if err := cmd.Run(); err != nil {
			return remoteTemplateDirs{}, fmt.Errorf("failed to init remote template bare repo: %w", err)
		}

		if err := repo.RunGitCommand("remote", "add", "origin", bareDir); err != nil {
			return remoteTemplateDirs{}, fmt.Errorf("failed to add remote to template repo: %w", err)
		}
		if err := repo.RunGitCommand("push", "-u", "origin", "main"); err != nil {
			return remoteTemplateDirs{}, fmt.Errorf("failed to push template repo to origin: %w", err)
		}

		return remoteTemplateDirs{repo: repoDir, bare: bareDir}, nil
	})
)

type remoteTemplateDirs struct {
	repo string
	bare string
}

func getMinimalTemplate(t *testing.T) string {
	t.Helper()

	dir, err := minimalTemplate()
	if err != nil {
		t.Fatalf("Minimal template initialization failed: %v", err)
	}
	return dir
}

func getBasicTemplate(t *testing.T) string {
	t.Helper()

	dir, err := basicTemplate()
	if err != nil {
		t.Fatalf("Basic template initialization failed: %v", err)
	}
	return dir
}

func getInitialCommitTemplate(t *testing.T) string {
	t.Helper()

	dir, err := initialCommitTemplate()
	if err != nil {
		t.Fatalf("Initial commit template initialization failed: %v", err)
	}
	return dir
}

func getRemoteTemplates(t *testing.T) (string, string) {
	t.Helper()

	dirs, err := remoteTemplates()
	if err != nil {
		t.Fatalf("Remote template initialization failed: %v", err)
	}
	return dirs.repo, dirs.bare
}

func setupMatches(setup SceneSetup, target SceneSetup) bool {
	if setup == nil || target == nil {
		return setup == nil && target == nil
	}
	return reflect.ValueOf(setup).Pointer() == reflect.ValueOf(target).Pointer()
}

func templateForSetup(t *testing.T, setup SceneSetup) (string, bool) {
	t.Helper()

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
	Dir  string
	Repo *GitRepo
}

// SceneSetup is a function type for setting up a scene.
type SceneSetup func(*Scene) error

// NewScene creates a new test scene with a temporary directory and Git repository,
// and changes the working directory into it for the rest of the test.
// It uses t.Chdir, so it cannot be combined with t.Parallel; use
// NewSceneParallel for tests that can run in parallel.
func NewScene(t *testing.T, setup SceneSetup) *Scene {
	t.Helper()

	scene := NewSceneParallel(t, setup)
	t.Chdir(scene.Dir)
	return scene
}

// NewSceneParallel creates a new test scene that is safe for parallel tests.
// Unlike NewScene, this does NOT change the working directory.
// Tests using this must ensure all git operations use explicit directory paths
// (e.g., via scene.Repo methods or cmd.Dir = scene.Dir).
func NewSceneParallel(t *testing.T, setup SceneSetup) *Scene {
	t.Helper()

	// Not t.TempDir: the cleanup below keeps the directory when DEBUG is set.
	tmpDir, err := os.MkdirTemp("", "stackit-test-*") //nolint:usetesting // see above
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

	tmpDir, err := os.MkdirTemp("", "stackit-test-*") //nolint:usetesting // kept when DEBUG is set; see NewSceneParallel
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
