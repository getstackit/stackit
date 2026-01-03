package testhelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	minimalTemplateDir  string
	minimalTemplateErr  error
	minimalTemplateOnce sync.Once

	basicTemplateDir  string
	basicTemplateErr  error
	basicTemplateOnce sync.Once
)

func getMinimalTemplate(t *testing.T) string {
	minimalTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "stackit-test-minimal-template-*")
		if err != nil {
			minimalTemplateErr = fmt.Errorf("failed to create minimal template dir: %w", err)
			return
		}
		minimalTemplateDir = dir

		// Initialize the minimal repo
		_, err = NewGitRepo(minimalTemplateDir)
		if err != nil {
			minimalTemplateErr = fmt.Errorf("failed to init minimal template repo: %w", err)
			return
		}
	})

	if minimalTemplateErr != nil {
		t.Fatalf("Minimal template initialization failed: %v", minimalTemplateErr)
	}

	return minimalTemplateDir
}

func getBasicTemplate(t *testing.T) string {
	basicTemplateOnce.Do(func() {
		minimalDir := getMinimalTemplate(t)

		dir, err := os.MkdirTemp("", "stackit-test-basic-template-*")
		if err != nil {
			basicTemplateErr = fmt.Errorf("failed to create basic template dir: %w", err)
			return
		}
		basicTemplateDir = dir

		// Clone from minimal
		repo, err := NewGitRepoFromTemplate(basicTemplateDir, minimalDir)
		if err != nil {
			basicTemplateErr = fmt.Errorf("failed to init basic template repo: %w", err)
			return
		}

		// Apply BasicSceneSetup
		if err := BasicSceneSetup(&Scene{Repo: repo, Dir: basicTemplateDir}); err != nil {
			basicTemplateErr = fmt.Errorf("failed to run basic setup on template: %w", err)
			return
		}
	})

	if basicTemplateErr != nil {
		t.Fatalf("Basic template initialization failed: %v", basicTemplateErr)
	}

	return basicTemplateDir
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

	// Initialize Git repository
	var repo *GitRepo
	var templateDir string
	isBasicSetup := false

	// Determine which template to use
	if setup != nil && fmt.Sprintf("%p", setup) == fmt.Sprintf("%p", BasicSceneSetup) {
		templateDir = getBasicTemplate(t)
		repo, err = NewGitRepoFromTemplate(tmpDir, templateDir)
		isBasicSetup = true
	} else {
		// All other cases (nil or custom setup) start with a minimal repo
		templateDir = getMinimalTemplate(t)
		repo, err = NewGitRepoFromTemplate(tmpDir, templateDir)
	}

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

	// Write default config files
	if err := scene.writeDefaultConfigs(); err != nil {
		_ = os.Chdir(oldDir)
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write config files: %v", err)
	}

	// Run custom setup if provided and not already covered by template
	if setup != nil && !isBasicSetup {
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

	// Initialize Git repository
	var repo *GitRepo
	var templateDir string
	isBasicSetup := false

	if setup != nil && fmt.Sprintf("%p", setup) == fmt.Sprintf("%p", BasicSceneSetup) {
		templateDir = getBasicTemplate(t)
		repo, err = NewGitRepoFromTemplate(tmpDir, templateDir)
		isBasicSetup = true
	} else {
		// All other cases (nil or custom setup) start with a minimal repo
		templateDir = getMinimalTemplate(t)
		repo, err = NewGitRepoFromTemplate(tmpDir, templateDir)
	}

	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create Git repo (template: %s, target: %s): %v", templateDir, tmpDir, err)
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

	// Run custom setup if provided and not already covered by template
	if setup != nil && !isBasicSetup {
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

	return nil
}

// BasicSceneSetup is a setup function that creates a basic scene with a single commit.
func BasicSceneSetup(scene *Scene) error {
	return scene.Repo.CreateChangeAndCommit("1", "1")
}
