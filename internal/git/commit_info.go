package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// resolveRefSHA resolves any ref form (branch, short SHA, full SHA, tag,
// "HEAD~1") to a 40-char SHA using `git rev-parse --verify`. The verify flag
// turns ambiguity into an error rather than a heuristic guess.
//
// On failure the returned error string contains "reference not found" so
// downstream callers that match on that legacy phrase continue to work.
func (r *runner) resolveRefSHA(ref string) (string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		// Fall back to the non-commit form: tags pointing at trees/blobs, or
		// generic ref lookups that aren't commits. Most stackit call sites
		// only care about commits, so try ^{commit} first to fail fast on
		// nonsense input.
		out, err = r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", "--end-of-options", ref)
		if err != nil {
			return "", fmt.Errorf("failed to resolve ref %s: reference not found: %w", ref, err)
		}
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("failed to resolve ref %s: reference not found", ref)
	}
	return sha, nil
}

func (r *runner) getCommitDate(branchName string) (time.Time, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", branchName)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get commit date for %s: %w", branchName, err)
	}
	s := strings.TrimSpace(out)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse commit date %q: %w", s, err)
	}
	return t, nil
}

func (r *runner) getCommitAuthor(branchName string) (string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%an", branchName)
	if err != nil {
		return "", fmt.Errorf("failed to get commit author for %s: %w", branchName, err)
	}
	return strings.TrimSpace(out), nil
}

// CommitInfo holds a branch tip commit's author date and author name.
type CommitInfo struct {
	Date   time.Time
	Author string
}

// batchCommitInfo resolves each branch's tip commit date and author in one
// `git for-each-ref` invocation instead of two `git log` processes per branch
// (getCommitDate + getCommitAuthor). Branches with no matching ref are simply
// absent from the result map rather than reported as errors, matching
// for-each-ref's own behavior for unmatched patterns.
func (r *runner) batchCommitInfo(branchNames []string) map[string]CommitInfo {
	results := make(map[string]CommitInfo)
	if len(branchNames) == 0 {
		return results
	}

	args := []string{"for-each-ref", "--format=%(refname:short)\t%(authordate:iso-strict)\t%(authorname)"}
	for _, name := range branchNames {
		args = append(args, "refs/heads/"+name)
	}

	out, err := r.RunGitCommandWithContext(context.Background(), args...)
	if err != nil {
		return results
	}

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		date, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		results[parts[0]] = CommitInfo{Date: date, Author: parts[2]}
	}
	return results
}

func (r *runner) getRevision(branchName string) (string, error) {
	return r.resolveRefSHA(branchName)
}

func (r *runner) getRemoteRevision(branchName string) (string, error) {
	// Use the bare "origin/<branch>" form so git's normal ref lookup order
	// applies (refs/heads/, refs/remotes/, etc.). Tests sometimes mock the
	// remote SHA by creating a local branch named "origin/<branch>" and
	// rely on that fallback resolving via refs/heads/.
	return r.resolveRefSHA("origin/" + branchName)
}

func (r *runner) batchGetRevisions(branchNames []string) (map[string]string, []error) {
	results := make(map[string]string)
	var errs []error

	if len(branchNames) == 0 {
		return results, errs
	}

	// Resolve all branches in one `git rev-parse` invocation. Each ref is
	// printed on its own line in the same order as the args, so we can map
	// back by index. `--verify` would short-circuit on the first bad ref, so
	// omit it and detect failures via empty/short output.
	args := append([]string{"rev-parse"}, branchNames...)
	out, err := r.RunGitCommandWithContext(context.Background(), args...)
	if err != nil {
		// Fall back to per-ref resolution so we can attribute errors to
		// specific branch names rather than a single bulk failure.
		for _, name := range branchNames {
			sha, e := r.resolveRefSHA(name)
			if e != nil {
				errs = append(errs, fmt.Errorf("failed to get revision for %s: %w", name, e))
				continue
			}
			results[name] = sha
		}
		return results, errs
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(branchNames) {
		// Output shape mismatch: surface a single error rather than misalign.
		return results, []error{fmt.Errorf("batch rev-parse returned %d lines for %d refs", len(lines), len(branchNames))}
	}
	for i, name := range branchNames {
		sha := strings.TrimSpace(lines[i])
		if sha == "" {
			errs = append(errs, fmt.Errorf("failed to get revision for %s: empty output", name))
			continue
		}
		results[name] = sha
	}
	return results, errs
}
