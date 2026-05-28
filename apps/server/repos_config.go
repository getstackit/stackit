package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reposConfig is the on-disk shape of the -repos-config file. The shape is
// modeled after a future "users add repos from GitHub" flow: each entry
// carries the GitHub owner/name coordinates, and the local checkout lives
// under a shared reposRoot at <reposRoot>/<owner>/<name>. The legacy
// absolute-path mode is preserved for one-off setups that can't sit under
// a shared root.
type reposConfig struct {
	// ReposRoot is the base directory under which per-repo checkouts live.
	// Can be overridden by the -repos-root flag or STACKIT_REPOS_ROOT env;
	// the resolved value is what loadReposConfig validates against.
	ReposRoot string       `json:"reposRoot,omitempty"`
	Repos     []repoConfig `json:"repos"`
}

type repoConfig struct {
	// ID is the URL-safe identifier used in /api/v1/repos/{repoID}/... .
	// It defaults to "<owner>-<name>" when owner/name are set and id is
	// omitted, so most config entries only need owner+name.
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`

	// Owner and Name are the GitHub coordinates for the repo. When set,
	// the local checkout is expected at <reposRoot>/<owner>/<name>. These
	// are the fields a future "add repo from GitHub" UI will populate.
	Owner string `json:"owner,omitempty"`
	Name  string `json:"name,omitempty"`

	// Path is an explicit override for the on-disk checkout location.
	// Absolute paths are used as-is; relative paths resolve against
	// reposRoot. When empty, Path is derived from <reposRoot>/<owner>/<name>.
	Path string `json:"path,omitempty"`

	Remote string `json:"remote,omitempty"`
}

// repoIDPattern restricts repo IDs to characters that survive in URL paths
// without escaping. Mirrors the registry's own constraint so config errors
// surface at startup, not at first request.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// loadReposConfig reads and validates the JSON repos config at path,
// applying rootOverride (from -repos-root / STACKIT_REPOS_ROOT) over the
// reposRoot field in the file. The returned config is normalized: every
// repo has an absolute Path, a non-empty ID/DisplayName, and a Remote.
func loadReposConfig(path, rootOverride string) (*reposConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read repos config %s: %w", path, err)
	}

	var cfg reposConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse repos config %s: %w", path, err)
	}
	if rootOverride != "" {
		cfg.ReposRoot = rootOverride
	}
	if cfg.ReposRoot != "" {
		abs, err := filepath.Abs(cfg.ReposRoot)
		if err != nil {
			return nil, fmt.Errorf("repos config %s: reposRoot %q: %w", path, cfg.ReposRoot, err)
		}
		cfg.ReposRoot = abs
	}

	if len(cfg.Repos) == 0 {
		return nil, fmt.Errorf("repos config %s: must list at least one repo", path)
	}

	seen := make(map[string]bool, len(cfg.Repos))
	for i := range cfg.Repos {
		r := &cfg.Repos[i]
		if err := normalizeRepoEntry(r, cfg.ReposRoot); err != nil {
			return nil, fmt.Errorf("repos config %s: repo[%d]: %w", path, i, err)
		}
		if !repoIDPattern.MatchString(r.ID) {
			return nil, fmt.Errorf("repos config %s: repo[%d] id %q must match %s", path, i, r.ID, repoIDPattern)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("repos config %s: duplicate id %q", path, r.ID)
		}
		seen[r.ID] = true
	}
	return &cfg, nil
}

// normalizeRepoEntry fills in defaults and resolves Path. It enforces that
// each entry has enough information to locate a checkout — either an
// explicit path or GitHub owner+name plus a reposRoot.
func normalizeRepoEntry(r *repoConfig, reposRoot string) error {
	r.Owner = strings.TrimSpace(r.Owner)
	r.Name = strings.TrimSpace(r.Name)
	r.Path = strings.TrimSpace(r.Path)

	if r.ID == "" {
		if r.Owner != "" && r.Name != "" {
			r.ID = r.Owner + "-" + r.Name
		} else {
			return fmt.Errorf("missing id (set id, or owner+name)")
		}
	}

	switch {
	case filepath.IsAbs(r.Path):
		// Absolute path: used verbatim.
	case r.Path != "":
		if reposRoot == "" {
			return fmt.Errorf("repo %q: relative path %q requires reposRoot", r.ID, r.Path)
		}
		r.Path = filepath.Join(reposRoot, r.Path)
	case r.Owner != "" && r.Name != "":
		if reposRoot == "" {
			return fmt.Errorf("repo %q: owner+name requires reposRoot (set -repos-root, STACKIT_REPOS_ROOT, or reposRoot in the config file)", r.ID)
		}
		r.Path = filepath.Join(reposRoot, r.Owner, r.Name)
	default:
		return fmt.Errorf("repo %q: must set path, or owner+name", r.ID)
	}

	if r.DisplayName == "" {
		switch {
		case r.Owner != "" && r.Name != "":
			r.DisplayName = r.Owner + "/" + r.Name
		default:
			r.DisplayName = r.ID
		}
	}
	if r.Remote == "" {
		r.Remote = "origin"
	}
	return nil
}
