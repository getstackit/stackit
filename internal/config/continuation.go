package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getstackit/stackit/internal/git"
)

// ContinuationState represents the state of a command that was interrupted by a rebase conflict
type ContinuationState struct {
	BranchesToRestack     []string `json:"branchesToRestack,omitempty"`
	BranchesToSync        []string `json:"branchesToSync,omitempty"` // For future sync command
	CurrentBranchOverride string   `json:"currentBranchOverride,omitempty"`
	RebasedBranchBase     string   `json:"rebasedBranchBase,omitempty"`
}

// GetContinuationState reads the continuation state from disk
func GetContinuationState(repoRoot string) (*ContinuationState, error) {
	gitDir := git.GetGitDir(repoRoot)
	configPath := filepath.Join(gitDir, ".stackit_continue")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no continuation state found")
		}
		return nil, fmt.Errorf("failed to read continuation state: %w", err)
	}

	var state ContinuationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse continuation state: %w", err)
	}
	return &state, nil
}

// PersistContinuationState writes the continuation state to disk
func PersistContinuationState(repoRoot string, state *ContinuationState) error {
	gitDir := git.GetGitDir(repoRoot)
	configPath := filepath.Join(gitDir, ".stackit_continue")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal continuation state: %w", err)
	}
	return os.WriteFile(configPath, data, 0600)
}

// ClearContinuationState removes the continuation state file
func ClearContinuationState(repoRoot string) error {
	gitDir := git.GetGitDir(repoRoot)
	configPath := filepath.Join(gitDir, ".stackit_continue")
	err := os.Remove(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear continuation state: %w", err)
	}
	return nil
}
