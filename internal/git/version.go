package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version identifies a Git release by the components Stackit needs for feature
// gates. Patch versions do not affect any currently supported gate.
type Version struct {
	Major int
	Minor int
}

// MinimumGitVersion is the oldest Git that can run stackit's stack operations.
// ListWorktrees needs `git worktree list -z`, which arrived in 2.36, and every
// ref-moving command inspects worktrees before it moves anything.
var MinimumGitVersion = Version{Major: 2, Minor: 36}

var gitVersionPattern = regexp.MustCompile(`git version (\d+)\.(\d+)`)

// AtLeast reports whether v meets or exceeds minimum.
func (v Version) AtLeast(minimum Version) bool {
	return v.Major > minimum.Major || (v.Major == minimum.Major && v.Minor >= minimum.Minor)
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// GitVersion reports the installed Git's major and minor version.
//
// A successful probe is cached for the runner's lifetime. A failed one is
// deliberately not: `git --version` can fail for reasons that say nothing about
// the installed version — a canceled context, a working directory that no
// longer exists — and caching that would make every later check report a
// perfectly modern Git as too old. In the CLI that means one bad probe poisons
// the command; in the long-lived server it poisons the repo's runner until the
// process restarts.
func (r *runner) GitVersion(ctx context.Context) (Version, error) {
	r.gitVersionMu.Lock()
	defer r.gitVersionMu.Unlock()

	if r.gitVersionParsed {
		return r.gitVersion, nil
	}

	version, err := r.probeGitVersion(ctx)
	if err != nil {
		return Version{}, err
	}

	r.gitVersion, r.gitVersionParsed = version, true
	return version, nil
}

func (r *runner) probeGitVersion(ctx context.Context) (Version, error) {
	output, err := r.RunGitCommandWithContext(ctx, "--version")
	if err != nil {
		return Version{}, fmt.Errorf("could not run git --version: %w", err)
	}
	return parseGitVersion(output)
}

// parseGitVersion pulls major.minor out of `git --version` output.
// Examples: "git version 2.39.2", "git version 2.35.1.windows.2".
func parseGitVersion(output string) (Version, error) {
	reported := strings.TrimSpace(output)
	matches := gitVersionPattern.FindStringSubmatch(reported)
	if len(matches) < 3 {
		return Version{}, fmt.Errorf("could not parse a version out of %q", reported)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return Version{}, fmt.Errorf("could not parse major version out of %q: %w", reported, err)
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return Version{}, fmt.Errorf("could not parse minor version out of %q: %w", reported, err)
	}

	return Version{Major: major, Minor: minor}, nil
}

// requireGitVersion reports why feature cannot run on the installed Git, or nil
// when it can.
//
// "could not determine the version" and "the version is too old" are reported
// differently on purpose. Telling someone on Git 2.49 to upgrade because their
// context was canceled sends them down entirely the wrong path, and these
// gates now sit in front of ListWorktrees — which nearly every stack command
// calls before it moves a ref.
func (r *runner) requireGitVersion(ctx context.Context, minimum Version, feature string) error {
	have, err := r.GitVersion(ctx)
	if err != nil {
		return fmt.Errorf("%s requires Git %d.%d or later, but the installed version could not be determined: %w",
			feature, minimum.Major, minimum.Minor, err)
	}

	if have.AtLeast(minimum) {
		return nil
	}

	return fmt.Errorf("%s requires Git %d.%d or later (found %d.%d); please upgrade your Git installation",
		feature, minimum.Major, minimum.Minor, have.Major, have.Minor)
}
