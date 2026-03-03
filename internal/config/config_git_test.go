package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

// removeDefaultConfig removes the default JSON config file created by test setup
// so we can test fresh/uninitialized state.
func removeDefaultConfig(t *testing.T, dir string) {
	t.Helper()
	configPath := filepath.Join(dir, ".git", ".stackit_config")
	_ = os.Remove(configPath)
}

func TestGitConfigBasicOperations(t *testing.T) {
	t.Parallel()

	t.Run("returns defaults when not initialized", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg.IsInitialized())
		require.Equal(t, DefaultTrunk, cfg.Trunk())
		require.True(t, cfg.SubmitFooter())
		require.Equal(t, DefaultUndoDepth, cfg.UndoStackDepth())
		require.Equal(t, DefaultCITimeout, cfg.CITimeout())
		require.Equal(t, DefaultSplitHunkSelector, cfg.SplitHunkSelector())
	})

	t.Run("sets and gets trunk", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetTrunk("develop")
		require.NoError(t, err)

		require.True(t, cfg.IsInitialized())
		require.Equal(t, "develop", cfg.Trunk())

		// Reload to verify persistence
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "develop", cfg2.Trunk())
	})

	t.Run("sets and gets submit footer", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSubmitFooter(false)
		require.NoError(t, err)
		require.False(t, cfg.SubmitFooter())

		// Reload to verify
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg2.SubmitFooter())
	})

	t.Run("sets and gets undo depth", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetUndoStackDepth(20)
		require.NoError(t, err)
		require.Equal(t, 20, cfg.UndoStackDepth())

		// Reload to verify
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 20, cfg2.UndoStackDepth())
	})

	t.Run("rejects invalid undo depth", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetUndoStackDepth(0)
		require.Error(t, err)

		err = cfg.SetUndoStackDepth(-1)
		require.Error(t, err)
	})
}

func TestGitConfigTrunks(t *testing.T) {
	t.Parallel()

	t.Run("manages multiple trunks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetTrunk("main")
		require.NoError(t, err)

		err = cfg.AddTrunk("develop")
		require.NoError(t, err)

		err = cfg.AddTrunk("staging")
		require.NoError(t, err)

		require.Equal(t, []string{"main", "develop", "staging"}, cfg.AllTrunks())
		require.True(t, cfg.IsTrunk("main"))
		require.True(t, cfg.IsTrunk("develop"))
		require.True(t, cfg.IsTrunk("staging"))
		require.False(t, cfg.IsTrunk("feature"))
	})

	t.Run("prevents duplicate trunks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetTrunk("main")
		require.NoError(t, err)

		err = cfg.AddTrunk("main")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already configured")
	})
}

func TestGitConfigMergeMethod(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Empty(t, cfg.MergeMethod())
	})

	t.Run("sets valid merge methods", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		for _, method := range []string{"squash", "merge", "rebase"} {
			err = cfg.SetMergeMethod(method)
			require.NoError(t, err)
			require.Equal(t, method, cfg.MergeMethod())
		}
	})

	t.Run("rejects invalid merge method", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetMergeMethod("invalid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid merge method")
	})
}

func TestGitConfigCISettings(t *testing.T) {
	t.Parallel()

	t.Run("sets and gets CI command", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetCICommand("mise run check")
		require.NoError(t, err)
		require.Equal(t, "mise run check", cfg.CICommand())
	})

	t.Run("sets and gets CI timeout", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetCITimeout(300)
		require.NoError(t, err)
		require.Equal(t, 300, cfg.CITimeout())
	})

	t.Run("rejects zero CI timeout", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// 0 is not allowed - use unset to revert to default
		err = cfg.SetCITimeout(0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "at least 1 second")
	})

	t.Run("rejects negative CI timeout", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetCITimeout(-1)
		require.Error(t, err)
	})
}

func TestGitConfigSplitHunkSelector(t *testing.T) {
	t.Parallel()

	t.Run("defaults to tui", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, "tui", cfg.SplitHunkSelector())
	})

	t.Run("sets valid selectors", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSplitHunkSelector("git")
		require.NoError(t, err)
		require.Equal(t, "git", cfg.SplitHunkSelector())

		err = cfg.SetSplitHunkSelector("tui")
		require.NoError(t, err)
		require.Equal(t, "tui", cfg.SplitHunkSelector())
	})

	t.Run("rejects invalid selector", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSplitHunkSelector("invalid")
		require.Error(t, err)
	})
}

func TestGitConfigBranchPattern(t *testing.T) {
	t.Parallel()

	t.Run("returns default pattern", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, DefaultBranchPattern.String(), cfg.BranchNamePattern())
	})

	t.Run("sets and gets valid pattern", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetBranchNamePattern("feature/{message}")
		require.NoError(t, err)
		require.Equal(t, "feature/{message}", cfg.BranchNamePattern())
	})

	t.Run("rejects invalid pattern", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// Pattern without {slug} is invalid
		err = cfg.SetBranchNamePattern("feature/")
		require.Error(t, err)
	})
}

func TestGitConfigApprovedHooks(t *testing.T) {
	t.Parallel()

	t.Run("manages approved hooks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Empty(t, cfg.ApprovedPostWorktreeCreateHooks())

		err = cfg.AddApprovedPostWorktreeCreateHook("mise trust")
		require.NoError(t, err)

		err = cfg.AddApprovedPostWorktreeCreateHook("npm install")
		require.NoError(t, err)

		require.True(t, cfg.IsPostWorktreeCreateHookApproved("mise trust"))
		require.True(t, cfg.IsPostWorktreeCreateHookApproved("npm install"))
		require.False(t, cfg.IsPostWorktreeCreateHookApproved("other"))

		err = cfg.RemoveApprovedPostWorktreeCreateHook("mise trust")
		require.NoError(t, err)

		require.False(t, cfg.IsPostWorktreeCreateHookApproved("mise trust"))
		require.True(t, cfg.IsPostWorktreeCreateHookApproved("npm install"))
	})

	t.Run("clears all hooks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddApprovedPostWorktreeCreateHook("mise trust")
		require.NoError(t, err)

		err = cfg.AddApprovedPostWorktreeCreateHook("npm install")
		require.NoError(t, err)

		err = cfg.ClearApprovedPostWorktreeCreateHooks()
		require.NoError(t, err)

		require.Empty(t, cfg.ApprovedPostWorktreeCreateHooks())
	})
}

func TestMigrationFromJSON(t *testing.T) {
	t.Parallel()

	t.Run("migrates full config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create JSON config (overwrite the default one created by test setup)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		footer := false
		depth := 15
		timeout := 300
		autoClean := false
		config := &RepoConfig{
			Trunk:                           new("develop"),
			Trunks:                          []string{"staging", "production"},
			BranchNamePattern:               new("feature/{message}"),
			SubmitFooter:                    &footer,
			UndoStackDepth:                  &depth,
			MergeMethod:                     new("squash"),
			CICommand:                       new("npm test"),
			CITimeout:                       &timeout,
			WorktreeBasePath:                new("/tmp/worktrees"),
			WorktreeAutoClean:               &autoClean,
			SplitHunkSelector:               new("git"),
			ApprovedPostWorktreeCreateHooks: []string{"mise trust"},
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		// Load should trigger migration
		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// Verify all values migrated
		require.Equal(t, "develop", cfg.Trunk())
		require.Equal(t, []string{"develop", "staging", "production"}, cfg.AllTrunks())
		require.Equal(t, "feature/{message}", cfg.BranchNamePattern())
		require.False(t, cfg.SubmitFooter())
		require.Equal(t, 15, cfg.UndoStackDepth())
		require.Equal(t, "squash", cfg.MergeMethod())
		require.Equal(t, "npm test", cfg.CICommand())
		require.Equal(t, 300, cfg.CITimeout())
		require.Equal(t, "/tmp/worktrees", cfg.WorktreeBasePath())
		require.False(t, cfg.WorktreeAutoClean())
		require.Equal(t, "git", cfg.SplitHunkSelector())
		require.True(t, cfg.IsPostWorktreeCreateHookApproved("mise trust"))

		// Verify backup file exists
		backupPath := filepath.Join(scene.Dir, ".git", ".stackit_config.migrated")
		_, err = os.Stat(backupPath)
		require.NoError(t, err)

		// Verify original file is gone
		_, err = os.Stat(configPath)
		require.True(t, os.IsNotExist(err))
	})

	t.Run("migrates legacy CI config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create JSON config with legacy combine.* fields
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		timeout := 120
		config := &RepoConfig{
			Trunk:            new("main"),
			CombineCICommand: new("make test"),
			CombineCITimeout: &timeout,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, "make test", cfg.CICommand())
		require.Equal(t, 120, cfg.CITimeout())
	})

	t.Run("prefers new CI config over legacy", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create JSON config with both new and legacy CI fields
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		newTimeout := 300
		legacyTimeout := 120
		config := &RepoConfig{
			Trunk:            new("main"),
			CICommand:        new("mise run check"),
			CITimeout:        &newTimeout,
			CombineCICommand: new("make test"),
			CombineCITimeout: &legacyTimeout,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, "mise run check", cfg.CICommand())
		require.Equal(t, 300, cfg.CITimeout())
	})

	t.Run("does not migrate if already migrated", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// First, set up git config directly
		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetTrunk("main")
		require.NoError(t, err)
		err = cfg.SetSubmitFooter(false)
		require.NoError(t, err)

		// Now create a JSON config that would migrate differently
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		footer := true
		config := &RepoConfig{
			Trunk:        new("develop"),
			SubmitFooter: &footer,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		// Load again - should NOT migrate because trunk already exists
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// Values should still be from git config, not JSON
		require.Equal(t, "main", cfg2.Trunk())
		require.False(t, cfg2.SubmitFooter())

		// JSON file should still exist (not moved to backup)
		_, err = os.Stat(configPath)
		require.NoError(t, err)
	})

	t.Run("handles missing JSON gracefully", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// No JSON config exists
		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg.IsInitialized())
	})

	t.Run("handles empty JSON config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create empty JSON config
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		err := os.WriteFile(configPath, []byte("{}"), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg.IsInitialized())
	})

	t.Run("handles corrupt JSON config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create corrupt JSON config
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		err := os.WriteFile(configPath, []byte("not valid json{"), 0600)
		require.NoError(t, err)

		// Should return error for corrupt JSON
		_, err = LoadGitConfig(scene.Dir)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse JSON config")
	})
}

func TestUnsetNonExistentKey(t *testing.T) {
	t.Parallel()

	t.Run("unset key that was never set returns no error", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// All unset methods should succeed even if key was never set
		require.NoError(t, cfg.UnsetTrunk())
		require.NoError(t, cfg.UnsetBranchNamePattern())
		require.NoError(t, cfg.UnsetSubmitFooter())
		require.NoError(t, cfg.UnsetMergeMethod())
		require.NoError(t, cfg.UnsetCICommand())
		require.NoError(t, cfg.UnsetCITimeout())
		require.NoError(t, cfg.UnsetMaxConcurrency())
	})
}

func TestGitConfigSaveNoop(t *testing.T) {
	t.Parallel()

	t.Run("Save is a no-op", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetTrunk("main")
		require.NoError(t, err)

		// Save should succeed but do nothing (writes are already persisted)
		err = cfg.Save()
		require.NoError(t, err)

		// Value should still be there
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "main", cfg2.Trunk())
	})
}

func TestGitConfigNavigation(t *testing.T) {
	t.Parallel()

	t.Run("returns defaults", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, DefaultNavigationWhen, cfg.NavigationWhen())
		require.Equal(t, DefaultNavigationMarker, cfg.NavigationMarker())
		require.Equal(t, DefaultNavigationLocation, cfg.NavigationLocation())
		require.Equal(t, DefaultNavigationShowMerged, cfg.NavigationShowMerged())
	})

	t.Run("sets valid navigation.when values", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		for _, when := range []string{"always", "never", "multiple"} {
			err = cfg.SetNavigationWhen(when)
			require.NoError(t, err)
			require.Equal(t, when, cfg.NavigationWhen())
		}
	})

	t.Run("rejects invalid navigation.when value", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationWhen("invalid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid navigation.when")
	})

	t.Run("sets valid navigation.location values", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		for _, location := range []string{"body", "comment"} {
			err = cfg.SetNavigationLocation(location)
			require.NoError(t, err)
			require.Equal(t, location, cfg.NavigationLocation())
		}
	})

	t.Run("rejects invalid navigation.location value", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationLocation("invalid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid navigation.location")
	})

	t.Run("sets navigation.marker", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationMarker("<--")
		require.NoError(t, err)
		require.Equal(t, "<--", cfg.NavigationMarker())
	})

	t.Run("accepts emoji markers counting runes not bytes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		// "👈👈👈" is 3 runes (12 bytes) - should be accepted since 3 <= 10
		err = cfg.SetNavigationMarker("👈👈👈")
		require.NoError(t, err)
		require.Equal(t, "👈👈👈", cfg.NavigationMarker())

		// 10 emoji runes (40 bytes) - should be accepted at the limit
		err = cfg.SetNavigationMarker("🔴🔴🔴🔴🔴🔴🔴🔴🔴🔴")
		require.NoError(t, err)

		// 11 emoji runes - should be rejected
		err = cfg.SetNavigationMarker("🔴🔴🔴🔴🔴🔴🔴🔴🔴🔴🔴")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed 10 characters")
	})

	t.Run("rejects empty navigation.marker", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationMarker("")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("rejects navigation.marker with newlines", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationMarker("arrow\n")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot contain newlines")
	})

	t.Run("rejects navigation.marker exceeding 10 chars", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationMarker("12345678901") // 11 chars
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot exceed 10 characters")
	})

	t.Run("sets and gets navigation.showMerged", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetNavigationShowMerged(true)
		require.NoError(t, err)
		require.True(t, cfg.NavigationShowMerged())

		err = cfg.SetNavigationShowMerged(false)
		require.NoError(t, err)
		require.False(t, cfg.NavigationShowMerged())
	})
}

func TestGitConfigWithProjectFallback(t *testing.T) {
	t.Parallel()

	t.Run("returns error for invalid project config YAML", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create invalid YAML project config
		projectConfig := "trunk: [invalid yaml\n"
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		_, err = LoadGitConfigWithProject(scene.Dir)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse")
	})

	t.Run("trunk falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunk
		projectConfig := "trunk: develop\n"
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.Equal(t, "develop", cfg.Trunk())

		// Set personal config
		err = cfg.SetTrunk("personal-main")
		require.NoError(t, err)

		// Should now use personal config
		require.Equal(t, "personal-main", cfg.Trunk())
	})

	t.Run("all trunks merges git and project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunks
		projectConfig := `trunk: main
trunks:
  - staging
  - production
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should include trunk and project trunks
		require.Equal(t, []string{"main", "staging", "production"}, cfg.AllTrunks())

		// Add personal trunk
		err = cfg.AddTrunk("develop")
		require.NoError(t, err)

		// Should include all trunks (deduplicated)
		require.Equal(t, []string{"main", "develop", "staging", "production"}, cfg.AllTrunks())
	})

	t.Run("branch pattern falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with branch pattern
		projectConfig := `branch:
  pattern: "feature/{message}"
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config pattern
		require.Equal(t, "feature/{message}", cfg.BranchNamePattern())

		// Set personal pattern
		err = cfg.SetBranchNamePattern("{username}/{message}")
		require.NoError(t, err)

		// Should now use personal pattern
		require.Equal(t, "{username}/{message}", cfg.BranchNamePattern())
	})

	t.Run("submit footer falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with footer disabled
		projectConfig := `submit:
  footer: false
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.False(t, cfg.SubmitFooter())

		// Set personal override
		err = cfg.SetSubmitFooter(true)
		require.NoError(t, err)

		// Should now use personal config
		require.True(t, cfg.SubmitFooter())
	})

	t.Run("merge method falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with merge method
		projectConfig := `merge:
  method: squash
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.Equal(t, "squash", cfg.MergeMethod())

		// Set personal override
		err = cfg.SetMergeMethod("rebase")
		require.NoError(t, err)

		// Should now use personal config
		require.Equal(t, "rebase", cfg.MergeMethod())
	})

	t.Run("CI command falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with CI command
		projectConfig := `ci:
  command: "make test"
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.Equal(t, "make test", cfg.CICommand())

		// Set personal override
		err = cfg.SetCICommand("npm test")
		require.NoError(t, err)

		// Should now use personal config
		require.Equal(t, "npm test", cfg.CICommand())
	})

	t.Run("CI timeout falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with CI timeout
		projectConfig := `ci:
  timeout: 300
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.Equal(t, 300, cfg.CITimeout())

		// Set personal override
		err = cfg.SetCITimeout(120)
		require.NoError(t, err)

		// Should now use personal config
		require.Equal(t, 120, cfg.CITimeout())
	})

	t.Run("returns defaults when neither git nor project config set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// No project config
		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, DefaultTrunk, cfg.Trunk())
		require.Equal(t, DefaultBranchPattern.String(), cfg.BranchNamePattern())
		require.Equal(t, DefaultSubmitFooter, cfg.SubmitFooter())
		require.Equal(t, DefaultCITimeout, cfg.CITimeout())
		require.Empty(t, cfg.MergeMethod())
		require.Empty(t, cfg.CICommand())
	})

	t.Run("LoadConfig uses project fallback", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config
		projectConfig := `trunk: develop
merge:
  method: squash
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		// LoadConfig should use project fallback
		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, "develop", cfg.Trunk())
		require.Equal(t, "squash", cfg.MergeMethod())
	})

	t.Run("deduplicates trunks from git and project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunks
		projectConfig := `trunk: main
trunks:
  - staging
  - production
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Add same trunk that's already in project config - should fail
		err = cfg.AddTrunk("staging")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already configured")
	})

	t.Run("RemoveTrunk gives helpful error for project config trunks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunks
		projectConfig := `trunk: main
trunks:
  - staging
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Try to remove trunk that's in project config
		err = cfg.RemoveTrunk("staging")
		require.Error(t, err)
		require.Contains(t, err.Error(), ".stackit.yaml")
	})

	t.Run("ClearTrunks only clears personal trunks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunks
		projectConfig := `trunk: main
trunks:
  - staging
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Add personal trunks
		err = cfg.AddTrunk("develop")
		require.NoError(t, err)
		err = cfg.AddTrunk("feature")
		require.NoError(t, err)

		// Should have all trunks
		require.Equal(t, []string{"main", "develop", "feature", "staging"}, cfg.AllTrunks())

		// Clear personal trunks
		err = cfg.ClearTrunks()
		require.NoError(t, err)

		// Should still have project trunks
		require.Equal(t, []string{"main", "staging"}, cfg.AllTrunks())
	})

	t.Run("UnsetTrunk reverts to project config trunk", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with trunk
		projectConfig := `trunk: develop
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project trunk
		require.Equal(t, "develop", cfg.Trunk())

		// Override with personal trunk
		err = cfg.SetTrunk("main")
		require.NoError(t, err)
		require.Equal(t, "main", cfg.Trunk())

		// Unset personal trunk
		err = cfg.UnsetTrunk()
		require.NoError(t, err)

		// Should revert to project trunk
		require.Equal(t, "develop", cfg.Trunk())
	})
}

func TestGitConfigSubmitDraft(t *testing.T) {
	t.Parallel()

	t.Run("returns default when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.False(t, cfg.SubmitDraft())
	})

	t.Run("sets and gets submit draft", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSubmitDraft(true)
		require.NoError(t, err)
		require.True(t, cfg.SubmitDraft())

		// Reload to verify persistence
		cfg2, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)
		require.True(t, cfg2.SubmitDraft())
	})

	t.Run("unsets submit draft", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSubmitDraft(true)
		require.NoError(t, err)

		err = cfg.UnsetSubmitDraft()
		require.NoError(t, err)

		require.False(t, cfg.SubmitDraft())
	})
}

func TestGitConfigSubmitWeb(t *testing.T) {
	t.Parallel()

	t.Run("returns default when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Equal(t, "never", cfg.SubmitWeb())
	})

	t.Run("sets valid values", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		for _, value := range []string{"always", "created", "never"} {
			err = cfg.SetSubmitWeb(value)
			require.NoError(t, err)
			require.Equal(t, value, cfg.SubmitWeb())
		}
	})

	t.Run("rejects invalid value", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.SetSubmitWeb("invalid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid submit.web")
	})
}

func TestGitConfigSubmitLabels(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Empty(t, cfg.SubmitLabels())
	})

	t.Run("adds labels", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitLabel("needs-review")
		require.NoError(t, err)
		err = cfg.AddSubmitLabel("bug")
		require.NoError(t, err)

		labels := cfg.SubmitLabels()
		require.Contains(t, labels, "needs-review")
		require.Contains(t, labels, "bug")
	})

	t.Run("prevents duplicate labels", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitLabel("needs-review")
		require.NoError(t, err)
		err = cfg.AddSubmitLabel("needs-review") // Add again
		require.NoError(t, err)

		labels := cfg.SubmitLabels()
		// Count occurrences of "needs-review"
		count := 0
		for _, l := range labels {
			if l == "needs-review" {
				count++
			}
		}
		require.Equal(t, 1, count, "Should not have duplicate labels")
	})

	t.Run("unsets labels", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitLabel("needs-review")
		require.NoError(t, err)

		err = cfg.UnsetSubmitLabels()
		require.NoError(t, err)

		require.Empty(t, cfg.SubmitLabels())
	})
}

func TestGitConfigSubmitReviewers(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Empty(t, cfg.SubmitReviewers())
	})

	t.Run("adds reviewers", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitReviewer("alice")
		require.NoError(t, err)
		err = cfg.AddSubmitReviewer("org/team")
		require.NoError(t, err)

		reviewers := cfg.SubmitReviewers()
		require.Contains(t, reviewers, "alice")
		require.Contains(t, reviewers, "org/team")
	})

	t.Run("prevents duplicate reviewers", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitReviewer("alice")
		require.NoError(t, err)
		err = cfg.AddSubmitReviewer("alice") // Add again
		require.NoError(t, err)

		reviewers := cfg.SubmitReviewers()
		count := 0
		for _, r := range reviewers {
			if r == "alice" {
				count++
			}
		}
		require.Equal(t, 1, count, "Should not have duplicate reviewers")
	})
}

func TestGitConfigSubmitAssignees(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		require.Empty(t, cfg.SubmitAssignees())
	})

	t.Run("adds assignees", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitAssignee("alice")
		require.NoError(t, err)
		err = cfg.AddSubmitAssignee("bob")
		require.NoError(t, err)

		assignees := cfg.SubmitAssignees()
		require.Contains(t, assignees, "alice")
		require.Contains(t, assignees, "bob")
	})

	t.Run("prevents duplicate assignees", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadGitConfig(scene.Dir)
		require.NoError(t, err)

		err = cfg.AddSubmitAssignee("alice")
		require.NoError(t, err)
		err = cfg.AddSubmitAssignee("alice") // Add again
		require.NoError(t, err)

		assignees := cfg.SubmitAssignees()
		count := 0
		for _, a := range assignees {
			if a == "alice" {
				count++
			}
		}
		require.Equal(t, 1, count, "Should not have duplicate assignees")
	})
}

func TestGitConfigSubmitWithProjectConfig(t *testing.T) {
	t.Parallel()

	t.Run("draft falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with draft enabled
		projectConfig := `submit:
  draft: true
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.True(t, cfg.SubmitDraft())

		// Override with personal setting
		err = cfg.SetSubmitDraft(false)
		require.NoError(t, err)
		require.False(t, cfg.SubmitDraft())

		// Unset personal setting
		err = cfg.UnsetSubmitDraft()
		require.NoError(t, err)

		// Should revert to project config
		require.True(t, cfg.SubmitDraft())
	})

	t.Run("labels merge git and project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with labels
		projectConfig := `submit:
  labels:
    - team-label
    - priority-high
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Add personal labels
		err = cfg.AddSubmitLabel("my-label")
		require.NoError(t, err)

		labels := cfg.SubmitLabels()
		require.Contains(t, labels, "my-label")
		require.Contains(t, labels, "team-label")
		require.Contains(t, labels, "priority-high")
	})

	t.Run("web falls back to project config", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		removeDefaultConfig(t, scene.Dir)

		// Create project config with web setting
		projectConfig := `submit:
  web: always
`
		err := os.WriteFile(filepath.Join(scene.Dir, ProjectConfigFileName), []byte(projectConfig), 0600)
		require.NoError(t, err)

		cfg, err := LoadGitConfigWithProject(scene.Dir)
		require.NoError(t, err)

		// Should use project config value
		require.Equal(t, "always", cfg.SubmitWeb())

		// Override with personal setting
		err = cfg.SetSubmitWeb("never")
		require.NoError(t, err)
		require.Equal(t, "never", cfg.SubmitWeb())

		// Unset personal setting
		err = cfg.UnsetSubmitWeb()
		require.NoError(t, err)

		// Should revert to project config
		require.Equal(t, "always", cfg.SubmitWeb())
	})
}
