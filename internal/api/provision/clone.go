package provision

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CloneParams describes a repository to clone for serving.
type CloneParams struct {
	// CloneURL is the source to clone from (e.g. https://github.com/owner/name.git).
	CloneURL string
	// Token authenticates the clone. It is passed to git via environment so it
	// never appears in argv (visible to `ps`) and is never written to the
	// cloned repo's config. Empty clones without authentication (public repos).
	Token string
	// Dest is the absolute checkout path, typically <reposRoot>/<owner>/<name>.
	Dest string
}

// EnsureClone clones CloneURL into Dest unless Dest is already a git checkout.
// It is idempotent: an existing checkout is treated as success and left
// untouched, so re-onboarding a repo or restarting after a partial run is safe.
//
// The token is supplied through GIT_CONFIG_* env vars as an http.extraHeader,
// not embedded in the remote URL — so a per-session user token is not persisted
// to .git/config where it would outlive the session and leak to disk.
func EnsureClone(ctx context.Context, p CloneParams) error {
	if isGitRepo(p.Dest) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Dest), 0o750); err != nil {
		return fmt.Errorf("create checkout parent: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--", p.CloneURL, p.Dest)
	// GIT_TERMINAL_PROMPT=0 makes a bad/empty token fail fast instead of
	// blocking on an interactive credential prompt.
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if p.Token != "" {
		header := "Authorization: Basic " + basicAuthHeader("x-access-token", p.Token)
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0="+header,
		)
	}
	cmd.Env = env

	if out, err := cmd.CombinedOutput(); err != nil {
		// Leave no half-written checkout behind so a retry starts clean.
		_ = os.RemoveAll(p.Dest)
		return fmt.Errorf("git clone %s: %w: %s", p.CloneURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isGitRepo reports whether dir looks like a git checkout (has a .git entry).
func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func basicAuthHeader(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
