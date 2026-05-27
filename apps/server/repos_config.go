package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// reposConfig is the on-disk shape of the -repos-config file. Multiple repos
// can be listed; each one becomes a separate registry entry at startup.
type reposConfig struct {
	Repos []repoConfig `json:"repos"`
}

type repoConfig struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Path        string `json:"path"`
	Remote      string `json:"remote,omitempty"`
}

// repoIDPattern restricts repo IDs to characters that survive in URL paths
// without escaping. Mirrors the registry's own constraint so config errors
// surface at startup, not at first request.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// loadReposConfig reads and validates the JSON repos config at path. It
// returns a normalized config (DisplayName and Remote defaulted) so callers
// don't need to re-check.
func loadReposConfig(path string) (*reposConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", path, err)
	}

	var cfg reposConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", path, err)
	}
	if len(cfg.Repos) == 0 {
		return nil, fmt.Errorf("repos config %s: must list at least one repo", path)
	}

	seen := make(map[string]bool, len(cfg.Repos))
	for i := range cfg.Repos {
		r := &cfg.Repos[i]
		if r.ID == "" {
			return nil, fmt.Errorf("repos config %s: repo[%d] missing id", path, i)
		}
		if !repoIDPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("repos config %s: repo[%d] id %q must match %s", path, i, r.ID, repoIDPattern)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("repos config %s: duplicate id %q", path, r.ID)
		}
		seen[r.ID] = true

		if r.Path == "" {
			return nil, fmt.Errorf("repos config %s: repo %q missing path", path, r.ID)
		}
		if r.DisplayName == "" {
			r.DisplayName = r.ID
		}
		if r.Remote == "" {
			r.Remote = "origin"
		}
	}
	return &cfg, nil
}
