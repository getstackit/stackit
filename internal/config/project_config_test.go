package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectConfig(t *testing.T) {
	t.Run("loads valid config with hooks", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `hooks:
  post-worktree-create:
    - mise trust
    - npm install
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, []string{"mise trust", "npm install"}, cfg.Hooks.For(PhasePostWorktreeCreate))
	})

	t.Run("returns empty config when file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, cfg.Hooks.For(PhasePostWorktreeCreate))
	})

	t.Run("returns error for malformed YAML", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `hooks:
  post-worktree-create:
    - [invalid
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
	})

	t.Run("handles empty hooks section", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `hooks:
  post-worktree-create: []
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, cfg.Hooks.For(PhasePostWorktreeCreate))
	})

	t.Run("handles empty config file", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(""), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, cfg.Hooks.For(PhasePostWorktreeCreate))
	})

	t.Run("rejects config with unknown top-level keys", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `other:
  field: value
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown key")
		assert.Contains(t, err.Error(), "other")
	})
}

func TestHooksConfigPhases(t *testing.T) {
	t.Parallel()

	t.Run("phase keys parse into one map", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		configContent := `hooks:
  post-worktree-create:
    - mise trust
  pre-modify:
    - scripts/check-review.sh
  post-submit:
    - scripts/notify.sh
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)

		assert.Equal(t, []string{"mise trust"}, cfg.Hooks.For(PhasePostWorktreeCreate))
		assert.Equal(t, []string{"scripts/check-review.sh"}, cfg.Hooks.For("pre-modify"))
		assert.Equal(t, []string{"scripts/notify.sh"}, cfg.Hooks.For("post-submit"))

		// For() abstracts the lookup so callers do not reach into the map directly.
		assert.Equal(t, []string{"mise trust"}, cfg.Hooks.For(PhasePostWorktreeCreate))
		assert.Equal(t, []string{"scripts/check-review.sh"}, cfg.Hooks.For("pre-modify"))
		assert.Nil(t, cfg.Hooks.For("never-configured"))
	})

	t.Run("worktree-create uses the same phase map", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		configContent := `hooks:
  post-worktree-create:
    - mise trust
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)

		assert.Equal(t, []string{"mise trust"}, cfg.Hooks.For(PhasePostWorktreeCreate))
		assert.Equal(t, []string{"mise trust"}, cfg.Hooks[PhasePostWorktreeCreate])
	})
}

func TestHasPostWorktreeCreateHooks(t *testing.T) {
	t.Run("returns true when hooks exist", func(t *testing.T) {
		cfg := &ProjectConfig{
			Hooks: HooksConfig{PhasePostWorktreeCreate: []string{"mise trust"}},
		}
		assert.True(t, cfg.HasPostWorktreeCreateHooks())
	})

	t.Run("returns false when no hooks", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasPostWorktreeCreateHooks())
	})

	t.Run("returns false when hooks list is empty", func(t *testing.T) {
		cfg := &ProjectConfig{
			Hooks: HooksConfig{PhasePostWorktreeCreate: []string{}},
		}
		assert.False(t, cfg.HasPostWorktreeCreateHooks())
	})
}

func TestLoadProjectConfigNewFields(t *testing.T) {
	t.Run("loads config with all team settings", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `trunk: develop
trunks:
  - staging
  - production

branch:
  pattern: "feature/{message}"

submit:
  footer: false

github:
  stack: true

merge:
  method: squash

ci:
  command: "make test"
  timeout: 300

hooks:
  post-worktree-create:
    - npm install
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)

		assert.Equal(t, "develop", cfg.Trunk)
		assert.Equal(t, []string{"staging", "production"}, cfg.Trunks)
		assert.Equal(t, "feature/{message}", cfg.Branch.Pattern)
		assert.False(t, cfg.GetSubmitFooter())
		assert.True(t, cfg.GetGitHubStack())
		assert.Equal(t, "squash", cfg.Merge.Method)
		assert.Equal(t, "make test", cfg.CI.Command)
		assert.Equal(t, 300, cfg.CI.Timeout)
		assert.Equal(t, []string{"npm install"}, cfg.Hooks.For(PhasePostWorktreeCreate))
	})

	t.Run("loads partial config", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `trunk: main
merge:
  method: rebase
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)

		assert.Equal(t, "main", cfg.Trunk)
		assert.Empty(t, cfg.Trunks)
		assert.Empty(t, cfg.Branch.Pattern)
		assert.Nil(t, cfg.Submit.Footer)
		assert.Equal(t, "rebase", cfg.Merge.Method)
		assert.Empty(t, cfg.CI.Command)
		assert.Zero(t, cfg.CI.Timeout)
	})
}

func TestProjectConfigHelpers(t *testing.T) {
	t.Run("HasTrunk", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasTrunk())

		cfg.Trunk = "main"
		assert.True(t, cfg.HasTrunk())
	})

	t.Run("HasTrunks", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasTrunks())

		cfg.Trunks = []string{"staging"}
		assert.True(t, cfg.HasTrunks())
	})

	t.Run("HasBranchPattern", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasBranchPattern())

		cfg.Branch.Pattern = "feature/{message}"
		assert.True(t, cfg.HasBranchPattern())
	})

	t.Run("HasSubmitFooter and GetSubmitFooter", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasSubmitFooter())
		assert.True(t, cfg.GetSubmitFooter()) // Default is true

		footer := true
		cfg.Submit.Footer = &footer
		assert.True(t, cfg.HasSubmitFooter())
		assert.True(t, cfg.GetSubmitFooter())

		footer = false
		cfg.Submit.Footer = &footer
		assert.True(t, cfg.HasSubmitFooter())
		assert.False(t, cfg.GetSubmitFooter())
	})

	t.Run("HasGitHubStack and GetGitHubStack", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasGitHubStack())
		assert.False(t, cfg.GetGitHubStack())

		enabled := true
		cfg.GitHub.Stack = &enabled
		assert.True(t, cfg.HasGitHubStack())
		assert.True(t, cfg.GetGitHubStack())
	})

	t.Run("HasMergeMethod", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasMergeMethod())

		cfg.Merge.Method = "squash"
		assert.True(t, cfg.HasMergeMethod())
	})

	t.Run("HasCICommand", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasCICommand())

		cfg.CI.Command = "make test"
		assert.True(t, cfg.HasCICommand())
	})

	t.Run("HasCITimeout", func(t *testing.T) {
		cfg := &ProjectConfig{}
		assert.False(t, cfg.HasCITimeout())

		cfg.CI.Timeout = 300
		assert.True(t, cfg.HasCITimeout())
	})
}

func TestProjectConfigValidation(t *testing.T) {
	t.Run("rejects duplicate trunk in trunks list", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `trunk: main
trunks:
  - main
  - develop
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate trunk")
		assert.Contains(t, err.Error(), "main")
	})

	t.Run("rejects invalid branch pattern", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Pattern without {slug} or {message} is invalid
		configContent := `branch:
  pattern: "feature/"
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid branch.pattern")
	})

	t.Run("rejects invalid merge method", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `merge:
  method: invalid
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid merge.method")
	})

	t.Run("rejects negative CI timeout", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `ci:
  timeout: -1
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid ci.timeout")
	})

	t.Run("accepts valid merge methods", func(t *testing.T) {
		for _, method := range []string{"squash", "merge", "rebase"} {
			tmpDir := t.TempDir()

			configContent := "merge:\n  method: " + method + "\n"
			err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
			require.NoError(t, err)

			cfg, err := LoadProjectConfig(tmpDir)
			require.NoError(t, err)
			assert.Equal(t, method, cfg.Merge.Method)
		}
	})

	t.Run("accepts valid branch pattern", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `branch:
  pattern: "feature/{message}"
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "feature/{message}", cfg.Branch.Pattern)
	})

	t.Run("validates stack shape", func(t *testing.T) {
		for _, shape := range []string{StackShapeTree, StackShapeLinear} {
			tmpDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte("stack:\n  shape: "+shape+"\n"), 0600))

			cfg, err := LoadProjectConfig(tmpDir)
			require.NoError(t, err)
			require.Equal(t, shape, cfg.Stack.Shape)
		}

		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte("stack:\n  shape: invalid\n"), 0600))
		_, err := LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid stack.shape")
	})

	t.Run("rejects invalid split.hunkSelector", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `split:
  hunkSelector: invalid
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid split.hunkSelector")
	})

	t.Run("accepts valid split.hunkSelector values", func(t *testing.T) {
		for _, selector := range []string{"tui", "git"} {
			tmpDir := t.TempDir()

			configContent := "split:\n  hunkSelector: " + selector + "\n"
			err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
			require.NoError(t, err)

			cfg, err := LoadProjectConfig(tmpDir)
			require.NoError(t, err)
			assert.Equal(t, selector, cfg.Split.HunkSelector)
		}
	})

	t.Run("rejects negative maxConcurrency", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `maxConcurrency: -1
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid maxConcurrency")
	})

	t.Run("accepts zero maxConcurrency", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `maxConcurrency: 0
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.True(t, cfg.HasMaxConcurrency())
		assert.Equal(t, 0, cfg.GetMaxConcurrency())
	})

	t.Run("accepts valid navigation config", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `navigation:
  when: multiple
  marker: "👈"
  location: comment
  showMerged: true
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "multiple", cfg.Navigation.When)
		assert.Equal(t, "👈", cfg.Navigation.Marker)
		assert.Equal(t, "comment", cfg.Navigation.Location)
		assert.True(t, cfg.HasNavigationShowMerged())
		assert.True(t, cfg.GetNavigationShowMerged())
	})

	t.Run("rejects invalid navigation.when", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `navigation:
  when: invalid
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid navigation.when")
	})

	t.Run("rejects invalid navigation.location", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `navigation:
  location: invalid
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid navigation.location")
	})

	t.Run("accepts location none", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `navigation:
  location: none
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "none", cfg.Navigation.Location)
	})

	t.Run("rejects navigation.marker with newlines", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := "navigation:\n  marker: \"arrow\\n\"\n"
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot contain newlines")
	})

	t.Run("rejects navigation.marker exceeding 10 characters", func(t *testing.T) {
		tmpDir := t.TempDir()

		configContent := `navigation:
  marker: "12345678901"
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		_, err = LoadProjectConfig(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot exceed 10 characters")
	})

	t.Run("accepts navigation.marker with emoji counting runes", func(t *testing.T) {
		tmpDir := t.TempDir()

		// 3 emoji runes (12 bytes) - should be accepted since 3 <= 10
		configContent := `navigation:
  marker: "👈👈👈"
`
		err := os.WriteFile(filepath.Join(tmpDir, ProjectConfigFileName), []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := LoadProjectConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "👈👈👈", cfg.Navigation.Marker)
	})
}
