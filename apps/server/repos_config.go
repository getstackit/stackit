package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// repoConfig is the resolved configuration for one served repo. Repos are
// sourced from the Postgres repo store (internal/api/store); each row is mapped
// into this shape and run through normalizeRepoEntry so path resolution,
// ID/DisplayName defaulting, and validation live in one place. The shape mirrors
// a "users add repos from GitHub" flow: an entry carries GitHub owner/name
// coordinates, and the local checkout lives under a shared reposRoot at
// <reposRoot>/<owner>/<name>. An explicit absolute Path is preserved for one-off
// setups that can't sit under a shared root.
type repoConfig struct {
	// ID is the URL-safe identifier used in /api/v1/repos/{repoID}/... .
	// It defaults to "<owner>-<name>" when owner/name are set and id is empty.
	ID          string
	DisplayName string

	// Owner and Name are the GitHub coordinates. When set, the local checkout
	// is expected at <reposRoot>/<owner>/<name>.
	Owner string
	Name  string

	// Path is an explicit override for the on-disk checkout location. Absolute
	// paths are used as-is; relative paths resolve against reposRoot. When empty,
	// Path is derived from <reposRoot>/<owner>/<name>.
	Path string

	Remote string

	// AddedBy is the GitHub login of the user who onboarded this repo, or
	// empty for operator-seeded rows. Carried through to the registry entry so
	// the server can scope per-user visibility.
	AddedBy string

	// Managed marks a server-owned mirror checkout the sync loop may
	// mirror-fetch. True for DB-backed repos; false for the -cwd dev repo.
	Managed bool
}

// repoIDPattern restricts repo IDs to characters that survive in URL paths
// without escaping. Mirrors the registry's own constraint so config errors
// surface at startup, not at first request.
var repoIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// normalizeRepoEntry fills in defaults and resolves Path. It enforces that each
// entry has enough information to locate a checkout — either an explicit path or
// GitHub owner+name plus a reposRoot.
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
			return fmt.Errorf("repo %q: owner+name requires reposRoot (set -repos-root or STACKIT_REPOS_ROOT)", r.ID)
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
