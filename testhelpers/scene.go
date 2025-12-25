package testhelpers

import (
	"os"
	"path/filepath"
	"testing"

	"stackit.dev/stackit/internal/git"
)

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
// This function is safe for parallel tests as it does NOT change the process working directory,
// but it does set the git package's working directory for proper operation.
func NewScene(t *testing.T, setup SceneSetup) *Scene {
	// Reset the default git repository to ensure this test gets a fresh one.
	// This is necessary because the git package uses a package-level global
	// for the repository, which would otherwise persist across tests.
	git.ResetDefaultRepo()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "stackit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Save current directory for compatibility (though we don't chdir anymore)
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Initialize Git repository
	repo, err := NewGitRepo(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create Git repo: %v", err)
	}

	scene := &Scene{
		Dir:    tmpDir,
		Repo:   repo,
		oldDir: oldDir,
	}

	// Write default config files
	if err := scene.writeDefaultConfigs(); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write config files: %v", err)
	}

	// Set the git working directory for this test
	git.SetWorkingDir(tmpDir)

	// Run custom setup if provided
	if setup != nil {
		if err := setup(scene); err != nil {
			git.SetWorkingDir("") // Reset on failure
			_ = os.RemoveAll(tmpDir)
			t.Fatalf("Setup failed: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		git.SetWorkingDir("") // Reset working directory
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

	// Initialize Git repository
	repo, err := NewGitRepo(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create Git repo: %v", err)
	}

	scene := &Scene{
		Dir:  tmpDir,
		Repo: repo,
	}

	// Write default config files
	if err := scene.writeDefaultConfigs(); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write config files: %v", err)
	}

	// Set the git working directory for this test
	git.SetWorkingDir(tmpDir)

	// Run custom setup if provided
	if setup != nil {
		if err := setup(scene); err != nil {
			git.SetWorkingDir("") // Reset on failure
			_ = os.RemoveAll(tmpDir)
			t.Fatalf("Setup failed: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		git.SetWorkingDir("") // Reset working directory
		if os.Getenv("DEBUG") == "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	return scene
}

// writeDefaultConfigs writes the default Stackit configuration files.
func (s *Scene) writeDefaultConfigs() error {
	// Write repo config (JSON format, matching cuteString output)
	repoConfigPath := filepath.Join(s.Dir, ".git", ".stackit_config")
	repoConfig := `{
  "trunk": "main",
  "isGithubIntegrationEnabled": false
}
`
	if err := os.WriteFile(repoConfigPath, []byte(repoConfig), 0600); err != nil {
		return err
	}

	// Write user config (JSON format)
	userConfigPath := filepath.Join(s.Dir, ".git", ".stackit_user_config")
	userConfig := `{
  "tips": false
}
`
	if err := os.WriteFile(userConfigPath, []byte(userConfig), 0600); err != nil {
		return err
	}

	// Set environment variable for user config path
	_ = os.Setenv("STACKIT_USER_CONFIG_PATH", userConfigPath)
	_ = os.Setenv("STACKIT_PROFILE", "")

	return nil
}

// BasicSceneSetup is a setup function that creates a basic scene with a single commit.
func BasicSceneSetup(scene *Scene) error {
	return scene.Repo.CreateChangeAndCommit("1", "1")
}
