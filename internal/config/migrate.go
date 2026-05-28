package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getstackit/stackit/internal/git"
)

const (
	jsonConfigFile   = ".stackit_config"
	jsonConfigBackup = ".stackit_config.migrated"
)

// hasContentToMigrate checks if the legacy config has any meaningful content worth migrating.
func hasContentToMigrate(cfg *RepoConfig) bool {
	// Primary indicator: trunk is set
	if cfg.Trunk != nil && *cfg.Trunk != "" {
		return true
	}
	// Secondary indicators: any other meaningful configuration
	if len(cfg.Trunks) > 0 {
		return true
	}
	if cfg.BranchNamePattern != nil && *cfg.BranchNamePattern != "" {
		return true
	}
	if cfg.SubmitFooter != nil {
		return true
	}
	if cfg.UndoStackDepth != nil {
		return true
	}
	if cfg.MaxConcurrency != nil {
		return true
	}
	if cfg.MergeMethod != nil && *cfg.MergeMethod != "" {
		return true
	}
	if cfg.CICommand != nil && *cfg.CICommand != "" {
		return true
	}
	if cfg.CombineCICommand != nil && *cfg.CombineCICommand != "" {
		return true
	}
	if len(cfg.ApprovedPostWorktreeCreateHooks) > 0 {
		return true
	}
	return false
}

// needsMigration checks if JSON config exists and git config doesn't have trunk set.
func needsMigration(repoRoot string) bool {
	gitDir := resolveGitDir(repoRoot)
	jsonPath := filepath.Join(gitDir, jsonConfigFile)

	// Check if JSON exists
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return false
	}

	// Check if already migrated (git config has trunk)
	store := git.NewConfigStore(repoRoot)
	return !store.Exists(KeyTrunk)
}

// migrateFromJSON migrates config from JSON file to git config.
// On success, the original JSON file is renamed to .stackit_config.migrated as a backup.
func migrateFromJSON(repoRoot string, store *git.ConfigStore) error {
	gitDir := resolveGitDir(repoRoot)
	jsonPath := filepath.Join(gitDir, jsonConfigFile)

	// Read JSON config
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("read JSON config: %w", err)
	}

	var oldConfig RepoConfig
	if err := json.Unmarshal(data, &oldConfig); err != nil {
		return fmt.Errorf("parse JSON config: %w", err)
	}

	// Check if config has any meaningful content worth migrating
	// If it's essentially empty (no trunk set), just remove it
	if !hasContentToMigrate(&oldConfig) {
		// Just rename the empty file to mark it as processed
		backupPath := filepath.Join(gitDir, jsonConfigBackup)
		if err := os.Rename(jsonPath, backupPath); err != nil {
			// If rename fails, try to remove. If that also fails, return error
			// to avoid repeated migration attempts
			if removeErr := os.Remove(jsonPath); removeErr != nil {
				return fmt.Errorf("failed to process empty config file: %w", errors.Join(err, removeErr))
			}
		}
		return nil
	}

	// Migrate each field
	if oldConfig.Trunk != nil && *oldConfig.Trunk != "" {
		if err := store.Set(KeyTrunk, *oldConfig.Trunk); err != nil {
			return fmt.Errorf("migrate trunk: %w", err)
		}
	}

	for _, trunk := range oldConfig.Trunks {
		if err := store.Add(KeyTrunks, trunk); err != nil {
			return fmt.Errorf("migrate additional trunk: %w", err)
		}
	}

	if oldConfig.BranchNamePattern != nil && *oldConfig.BranchNamePattern != "" {
		if err := store.Set(KeyBranchPattern, *oldConfig.BranchNamePattern); err != nil {
			return fmt.Errorf("migrate branch pattern: %w", err)
		}
	}

	if oldConfig.SubmitFooter != nil {
		if err := store.SetBool(KeySubmitFooter, *oldConfig.SubmitFooter); err != nil {
			return fmt.Errorf("migrate submit footer: %w", err)
		}
	}

	if oldConfig.UndoStackDepth != nil {
		if err := store.SetInt(KeyUndoDepth, *oldConfig.UndoStackDepth); err != nil {
			return fmt.Errorf("migrate undo depth: %w", err)
		}
	}

	if oldConfig.MaxConcurrency != nil {
		if err := store.SetInt(KeyMaxConcurrency, *oldConfig.MaxConcurrency); err != nil {
			return fmt.Errorf("migrate max concurrency: %w", err)
		}
	}

	if oldConfig.WorktreeBasePath != nil && *oldConfig.WorktreeBasePath != "" {
		if err := store.Set(KeyWorktreeBasePath, *oldConfig.WorktreeBasePath); err != nil {
			return fmt.Errorf("migrate worktree base path: %w", err)
		}
	}

	if oldConfig.WorktreeAutoClean != nil {
		if err := store.SetBool(KeyWorktreeAutoClean, *oldConfig.WorktreeAutoClean); err != nil {
			return fmt.Errorf("migrate worktree auto clean: %w", err)
		}
	}

	if oldConfig.MergeMethod != nil && *oldConfig.MergeMethod != "" {
		if err := store.Set(KeyMergeMethod, *oldConfig.MergeMethod); err != nil {
			return fmt.Errorf("migrate merge method: %w", err)
		}
	}

	// Migrate unified CI config (prefer new over legacy)
	ciCommand := ""
	if oldConfig.CICommand != nil && *oldConfig.CICommand != "" {
		ciCommand = *oldConfig.CICommand
	} else if oldConfig.CombineCICommand != nil && *oldConfig.CombineCICommand != "" {
		ciCommand = *oldConfig.CombineCICommand
	}
	if ciCommand != "" {
		if err := store.Set(KeyCICommand, ciCommand); err != nil {
			return fmt.Errorf("migrate CI command: %w", err)
		}
	}

	ciTimeout := 0
	if oldConfig.CITimeout != nil && *oldConfig.CITimeout > 0 {
		ciTimeout = *oldConfig.CITimeout
	} else if oldConfig.CombineCITimeout != nil && *oldConfig.CombineCITimeout > 0 {
		ciTimeout = *oldConfig.CombineCITimeout
	}
	if ciTimeout > 0 {
		if err := store.SetInt(KeyCITimeout, ciTimeout); err != nil {
			return fmt.Errorf("migrate CI timeout: %w", err)
		}
	}

	if oldConfig.SplitHunkSelector != nil && *oldConfig.SplitHunkSelector != "" {
		if err := store.Set(KeySplitHunkSelector, *oldConfig.SplitHunkSelector); err != nil {
			return fmt.Errorf("migrate split hunk selector: %w", err)
		}
	}

	// Migrate approved hooks. Written under the per-phase key; the read path
	// still understands the legacy single-key form for older repos that didn't
	// go through this migration.
	for _, hook := range oldConfig.ApprovedPostWorktreeCreateHooks {
		if err := store.Add(KeyApprovedHookPrefix+PhasePostWorktreeCreate, hook); err != nil {
			return fmt.Errorf("migrate approved hook: %w", err)
		}
	}

	// Backup old JSON config
	backupPath := filepath.Join(gitDir, jsonConfigBackup)
	if err := os.Rename(jsonPath, backupPath); err != nil {
		return fmt.Errorf("backup old config: %w", err)
	}

	// Migration is silent - the backup file's existence indicates migration occurred.
	// Users can check for .stackit_config.migrated if they need to know.

	return nil
}
