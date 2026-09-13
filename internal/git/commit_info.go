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
// stackitRefPrefix is the namespace holding stackit's metadata refs, which
// point at blobs rather than commits.
const stackitRefPrefix = "refs/stackit/"

func (r *runner) resolveRefSHA(ref string) (string, error) {
	var out string
	var err error

	// Refs under refs/stackit/ address metadata blobs, never commits, so the
	// ^{commit} peel can only ever fail for them — it was costing a guaranteed
	// wasted git process on every metadata read. Skipping it is safe beyond
	// blobs too: peeling is only load-bearing for annotated tags, and no tag
	// lives in this namespace.
	if !strings.HasPrefix(ref, stackitRefPrefix) {
		out, err = r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	}
	if err != nil || out == "" {
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

// CommitInfo holds a branch tip commit's author date and author name.
type CommitInfo struct {
	Date   time.Time
	Author string
}

// batchCommitInfo keeps local branches on a one-process fast path. Other refs
// (HEAD, tags, SHAs, revision expressions) are peeled and read in bulk. Invalid
// or non-commit refs are omitted; results are keyed by the original input.
func (r *runner) batchCommitInfo(branchNames []string) map[string]CommitInfo {
	results := make(map[string]CommitInfo)
	if len(branchNames) == 0 {
		return results
	}

	// %(refname), not %(refname:short): the short form disambiguates to
	// "heads/<name>" when a tag shares the branch's name, and the caller looks
	// results up by bare branch name — a miss silently yields a zero CommitInfo.
	args := []string{"for-each-ref", "--format=%(refname)\t%(authordate:iso-strict)\t%(authorname)"}
	requested := make(map[string]bool, len(branchNames))
	for _, name := range branchNames {
		if requested[name] || name == "" || strings.ContainsAny(name, "\r\n") {
			continue
		}
		requested[name] = true
		args = append(args, "refs/heads/"+name)
	}
	if len(requested) == 0 {
		return results
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
		name := strings.TrimPrefix(parts[0], "refs/heads/")
		if requested[name] {
			results[name] = CommitInfo{Date: date, Author: parts[2]}
		}
	}
	r.readOtherCommitInfo(requested, results)
	return results
}

func (r *runner) readOtherCommitInfo(requested map[string]bool, results map[string]CommitInfo) {
	var missing []string
	var input strings.Builder
	for name := range requested {
		if _, ok := results[name]; !ok {
			missing = append(missing, name)
			input.WriteString(name + "^{commit}\n")
		}
	}
	if len(missing) == 0 {
		return
	}
	// Batch-check isolates invalid refs without per-ref retries.
	out, err := r.runGitInternal(context.Background(), input.String(), nil, false, "cat-file", "--batch-check=%(objectname)")
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(missing) {
		return
	}
	bySHA := make(map[string][]string)
	args := []string{gitCmdLog, "--no-walk=unsorted", "--format=%H%x00%aI%x00%an", "--end-of-options"}
	for i, sha := range lines {
		if strings.ContainsAny(sha, " \t") || sha == "" {
			continue // "<ref> missing" or "<ref> ambiguous"
		}
		if len(bySHA[sha]) == 0 {
			args = append(args, sha)
		}
		bySHA[sha] = append(bySHA[sha], missing[i])
	}
	if len(bySHA) == 0 {
		return
	}
	args = append(args, "--")
	out, err = r.RunGitCommandWithContext(context.Background(), args...)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		date, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		for _, name := range bySHA[parts[0]] {
			results[name] = CommitInfo{Date: date, Author: parts[2]}
		}
	}
}

func (r *runner) getRevision(branchName string) (string, error) {
	return r.resolveRefSHA(branchName)
}

func (r *runner) getRemoteRevision(branchName string) (string, error) {
	// Use the bare "<remote>/<branch>" form so git's normal ref lookup order
	// applies (refs/heads/, refs/remotes/, etc.). Tests sometimes mock the
	// remote SHA by creating a local branch named "origin/<branch>" and
	// rely on that fallback resolving via refs/heads/.
	return r.resolveRefSHA(r.getRemote() + "/" + branchName)
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
