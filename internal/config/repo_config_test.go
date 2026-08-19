package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestConfigSubmitFooter(t *testing.T) {
	t.Parallel()

	t.Run("returns true when config does not exist", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.True(t, cfg.SubmitFooter())
	})

	t.Run("returns true when config exists but submit.footer is not set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create config file without submit.footer
		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		config := &RepoConfig{
			Trunk: new("main"),
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.True(t, cfg.SubmitFooter())
	})

	t.Run("returns true when config has submit.footer set to true", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create config file with submit.footer = true
		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		enabled := true
		config := &RepoConfig{
			Trunk:        new("main"),
			SubmitFooter: &enabled,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.True(t, cfg.SubmitFooter())
	})

	t.Run("returns false when config has submit.footer set to false", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Create config file with submit.footer = false
		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		enabled := false
		config := &RepoConfig{
			Trunk:        new("main"),
			SubmitFooter: &enabled,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg.SubmitFooter())
	})
}

func TestConfigSetSubmitFooter(t *testing.T) {
	t.Parallel()

	t.Run("sets submit.footer to true", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetSubmitFooter(true)
		require.NoError(t, err)
		err = cfg.Save()
		require.NoError(t, err)

		// Verify Config.SubmitFooter returns true by reloading
		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.True(t, cfg2.SubmitFooter())
	})

	t.Run("sets submit.footer to false", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetSubmitFooter(false)
		require.NoError(t, err)
		err = cfg.Save()
		require.NoError(t, err)

		// Verify Config.SubmitFooter returns false by reloading
		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, cfg2.SubmitFooter())
	})

	t.Run("updates existing config without overwriting other fields", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Set trunk first
		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetTrunk("main")
		require.NoError(t, err)

		// Set submit.footer
		err = cfg.SetSubmitFooter(false)
		require.NoError(t, err)
		err = cfg.Save()
		require.NoError(t, err)

		// Verify both fields are present by reloading
		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "main", cfg2.Trunk())
		require.False(t, cfg2.SubmitFooter())
	})
}

func TestConfigWorktreeSupport(t *testing.T) {
	t.Parallel()

	t.Run("loads config from worktree using main repo git dir", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.BasicSceneSetup)

		// Create and save config in main repo
		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetTrunk("main")
		require.NoError(t, err)
		err = cfg.SetSubmitFooter(false)
		require.NoError(t, err)

		// Create a branch for the worktree
		err = scene.Repo.CreateBranch("feature-branch")
		require.NoError(t, err)

		// Create worktree (normalize path for macOS /var -> /private/var symlink)
		tmpDir := t.TempDir()
		worktreePath, err := filepath.EvalSymlinks(tmpDir)
		require.NoError(t, err)
		worktreePath = filepath.Join(worktreePath, "worktree")
		err = scene.Repo.RunGitCommand("worktree", "add", worktreePath, "feature-branch")
		require.NoError(t, err)

		// Load config from worktree - should find main repo's config
		worktreeCfg, err := LoadConfig(worktreePath)
		require.NoError(t, err)
		require.True(t, worktreeCfg.IsInitialized())
		require.Equal(t, "main", worktreeCfg.Trunk())
		require.False(t, worktreeCfg.SubmitFooter())
	})

	t.Run("saves config from worktree to main repo git dir", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.BasicSceneSetup)

		// Initialize config in main repo
		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		err = cfg.SetTrunk("main")
		require.NoError(t, err)

		// Create a branch for the worktree
		err = scene.Repo.CreateBranch("feature-branch")
		require.NoError(t, err)

		// Create worktree (normalize path for macOS /var -> /private/var symlink)
		tmpDir := t.TempDir()
		worktreePath, err := filepath.EvalSymlinks(tmpDir)
		require.NoError(t, err)
		worktreePath = filepath.Join(worktreePath, "worktree")
		err = scene.Repo.RunGitCommand("worktree", "add", worktreePath, "feature-branch")
		require.NoError(t, err)

		// Modify config from worktree
		worktreeCfg, err := LoadConfig(worktreePath)
		require.NoError(t, err)
		err = worktreeCfg.SetSubmitFooter(false)
		require.NoError(t, err)

		// Verify change is visible from main repo
		mainCfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.False(t, mainCfg.SubmitFooter())
	})
}

func TestConfigCICommand(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when nothing configured", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "", cfg.CICommand())
	})

	t.Run("returns ci.command when set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		config := &RepoConfig{
			Trunk:     new("main"),
			CICommand: new("mise run check"),
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "mise run check", cfg.CICommand())
	})

	t.Run("falls back to combine.ciCommand for backwards compatibility", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		config := &RepoConfig{
			Trunk:            new("main"),
			CombineCICommand: new("npm test"),
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "npm test", cfg.CICommand())
	})

	t.Run("ci.command takes precedence over combine.ciCommand", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		config := &RepoConfig{
			Trunk:            new("main"),
			CICommand:        new("mise run check"),
			CombineCICommand: new("npm test"),
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "mise run check", cfg.CICommand())
	})
}

func TestConfigSetCICommand(t *testing.T) {
	t.Parallel()

	t.Run("sets ci.command", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		cfg.SetCICommand("make test")
		err = cfg.Save()
		require.NoError(t, err)

		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, "make test", cfg2.CICommand())
	})
}

func TestConfigCITimeout(t *testing.T) {
	t.Parallel()

	t.Run("returns default 600 when nothing configured", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 600, cfg.CITimeout())
	})

	t.Run("returns ci.timeout when set", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		timeout := 300
		config := &RepoConfig{
			Trunk:     new("main"),
			CITimeout: &timeout,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 300, cfg.CITimeout())
	})

	t.Run("falls back to combine.ciTimeout for backwards compatibility", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		timeout := 120
		config := &RepoConfig{
			Trunk:            new("main"),
			CombineCITimeout: &timeout,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 120, cfg.CITimeout())
	})

	t.Run("ci.timeout takes precedence over combine.ciTimeout", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Migration only acts on a repo that has not been initialized in git config.
		testhelpers.MakeUninitialized(t, scene.Dir)
		configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
		newTimeout := 180
		legacyTimeout := 300
		config := &RepoConfig{
			Trunk:            new("main"),
			CITimeout:        &newTimeout,
			CombineCITimeout: &legacyTimeout,
		}
		configJSON, err := json.MarshalIndent(config, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(configPath, configJSON, 0600)
		require.NoError(t, err)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 180, cfg.CITimeout())
	})
}

func TestConfigSetCITimeout(t *testing.T) {
	t.Parallel()

	t.Run("sets ci.timeout", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		cfg.SetCITimeout(900)
		err = cfg.Save()
		require.NoError(t, err)

		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, 900, cfg2.CITimeout())
	})
}

func TestConfigApprovedHooks_LegacyJSONMigration(t *testing.T) {
	t.Parallel()

	// Legacy .stackit_config JSON files predate per-phase git config keys.
	// The migration path must surface ApprovedPostWorktreeCreateHooks under
	// the new per-phase API so older repos keep working.
	scene := testhelpers.NewSceneParallel(t, nil)

	// Migration only acts on a repo that has not been initialized in git config.
	testhelpers.MakeUninitialized(t, scene.Dir)
	configPath := filepath.Join(scene.Dir, ".git", ".stackit_config")
	legacy := &RepoConfig{
		Trunk:                           new("main"),
		ApprovedPostWorktreeCreateHooks: []string{"mise trust", "npm install"},
	}
	configJSON, err := json.MarshalIndent(legacy, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, configJSON, 0600))

	cfg, err := LoadConfig(scene.Dir)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"mise trust", "npm install"}, cfg.ApprovedHooks(PhasePostWorktreeCreate))
	require.True(t, cfg.IsHookApproved(PhasePostWorktreeCreate, "mise trust"))
	require.False(t, cfg.IsHookApproved(PhasePostWorktreeCreate, "never-approved"))
}

func TestConfigApprovedHooks_GenericPhase(t *testing.T) {
	t.Parallel()

	t.Run("add and read approvals for an arbitrary phase", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)

		require.False(t, cfg.IsHookApproved("pre-modify", "scripts/check.sh"))
		require.NoError(t, cfg.AddApprovedHook("pre-modify", "scripts/check.sh"))
		require.True(t, cfg.IsHookApproved("pre-modify", "scripts/check.sh"))

		cfg2, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, []string{"scripts/check.sh"}, cfg2.ApprovedHooks("pre-modify"))
	})

	t.Run("approvals are scoped per phase", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)

		require.NoError(t, cfg.AddApprovedHook("pre-modify", "shared.sh"))
		require.True(t, cfg.IsHookApproved("pre-modify", "shared.sh"))
		require.False(t, cfg.IsHookApproved("pre-submit", "shared.sh"))
	})

	t.Run("remove and clear work for arbitrary phases", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)

		require.NoError(t, cfg.AddApprovedHook("pre-modify", "a"))
		require.NoError(t, cfg.AddApprovedHook("pre-modify", "b"))
		require.NoError(t, cfg.RemoveApprovedHook("pre-modify", "a"))
		require.Equal(t, []string{"b"}, cfg.ApprovedHooks("pre-modify"))

		require.NoError(t, cfg.ClearApprovedHooks("pre-modify"))
		require.Empty(t, cfg.ApprovedHooks("pre-modify"))
	})
}

func TestConfigApprovedHooks_LegacyKeyCompat(t *testing.T) {
	t.Parallel()

	t.Run("legacy key entries surface via the new generic reader", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		// Write directly to the legacy single-key form.
		store := git.NewConfigStore(scene.Dir)
		require.NoError(t, store.Add(KeyApprovedHooks, "legacy-only"))

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.Equal(t, []string{"legacy-only"}, cfg.ApprovedHooks(PhasePostWorktreeCreate))
		require.True(t, cfg.IsHookApproved(PhasePostWorktreeCreate, "legacy-only"))
	})

	t.Run("union of legacy + per-phase keys deduplicates", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		store := git.NewConfigStore(scene.Dir)
		require.NoError(t, store.Add(KeyApprovedHooks, "shared"))
		require.NoError(t, store.Add(KeyApprovedHooks, "legacy-only"))

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.NoError(t, cfg.AddApprovedHook(PhasePostWorktreeCreate, "shared"))
		require.NoError(t, cfg.AddApprovedHook(PhasePostWorktreeCreate, "new-only"))

		got := cfg.ApprovedHooks(PhasePostWorktreeCreate)
		require.ElementsMatch(t, []string{"shared", "legacy-only", "new-only"}, got)
		require.Len(t, got, 3, "shared entry should appear once even though it's in both keys")
	})

	t.Run("remove for legacy phase strips from both keys", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)

		store := git.NewConfigStore(scene.Dir)
		require.NoError(t, store.Add(KeyApprovedHooks, "drop-me"))

		cfg, err := LoadConfig(scene.Dir)
		require.NoError(t, err)
		require.NoError(t, cfg.AddApprovedHook(PhasePostWorktreeCreate, "drop-me"))

		require.NoError(t, cfg.RemoveApprovedHook(PhasePostWorktreeCreate, "drop-me"))
		require.False(t, cfg.IsHookApproved(PhasePostWorktreeCreate, "drop-me"))

		// Verify both stores are empty.
		legacy, _ := store.GetAll(KeyApprovedHooks)
		require.Empty(t, legacy)
		newKey, _ := store.GetAll(KeyApprovedHookPrefix + PhasePostWorktreeCreate)
		require.Empty(t, newKey)
	})
}
