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
	return r.resolveRefSHAContext(context.Background(), ref)
}

func (r *runner) resolveRefSHAContext(ctx context.Context, ref string) (string, error) {
	var out string
	var err error

	// Refs under refs/stackit/ address metadata blobs, never commits, so the
	// ^{commit} peel can only ever fail for them — it was costing a guaranteed
	// wasted git process on every metadata read. Skipping it is safe beyond
	// blobs too: peeling is only load-bearing for annotated tags, and no tag
	// lives in this namespace.
	if !strings.HasPrefix(ref, stackitRefPrefix) {
		out, err = r.RunGitCommandWithContext(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	}
	if err != nil || out == "" {
		// Fall back to the non-commit form: tags pointing at trees/blobs, or
		// generic ref lookups that aren't commits. Most stackit call sites
		// only care about commits, so try ^{commit} first to fail fast on
		// nonsense input.
		out, err = r.RunGitCommandWithContext(ctx, "rev-parse", "--verify", "--end-of-options", ref)
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

	// %(refname), not %(refname:short): the short form disambiguates to
	// "heads/<name>" when a tag shares the branch's name, and the caller looks
	// results up by bare branch name — a miss silently yields a zero CommitInfo.
	args := []string{"for-each-ref", "--format=%(refname)\t%(authordate:iso-strict)\t%(authorname)"}
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
		results[strings.TrimPrefix(parts[0], "refs/heads/")] = CommitInfo{Date: date, Author: parts[2]}
	}
	return results
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
		// Missing remote-tracking refs are normal for unpublished branches.
		// Keep those lookups batched, with a result for every requested ref.
		// Only unusual revision expressions containing protocol delimiters
		// need the individual path (e.g. HEAD:path with a newline in path).
		batchSafe := true
		for _, name := range branchNames {
			if strings.ContainsAny(name, "\n\x00") {
				batchSafe = false
				break
			}
		}
		if batchSafe {
			return r.batchResolveRevisions(branchNames)
		}
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

// batchResolveRevisions preserves resolveRefSHA's commit peeling and raw-object
// fallback in one cat-file process. Unlike rev-parse, a missing ref produces a
// response without aborting the rest of the batch.
func (r *runner) batchResolveRevisions(names []string) (map[string]string, []error) {
	result := r.ReadRevisions(context.Background(), names...)
	var errs []error
	for _, name := range names {
		if err := result.Errors[name]; err != nil {
			errs = append(errs, err)
		}
	}
	return result.Values, errs
}

// ReadRevisions resolves refs with one contract for one or many inputs: commit
// tags are peeled, other objects are returned as-is, and missing refs have
// individual errors. Metadata refs are always read without peeling.
func (r *runner) ReadRevisions(ctx context.Context, names ...string) ReadResults[string] {
	result := ReadResults[string]{Values: make(map[string]string), Errors: make(map[string]error)}
	if len(names) == 0 {
		return result
	}
	if err := ctx.Err(); err != nil {
		for _, name := range names {
			result.Errors[name] = err
		}
		return result
	}
	if len(names) == 1 {
		name := names[0]
		if name == "HEAD" {
			if sha, ok := r.readHeadRevision(); ok {
				result.Values[name] = sha
				return result
			}
		}
		sha, err := r.resolveRefSHAContext(ctx, name)
		if err != nil {
			result.Errors[name] = err
		} else {
			result.Values[name] = sha
		}
		return result
	}
	queries := make([]string, 0, len(names)*2)
	offsets := make([]int, 0, len(names)+1)
	batchNames := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		// The line protocol cannot encode newlines in revision expressions.
		if strings.ContainsAny(name, "\n\x00") {
			sha, err := r.resolveRefSHAContext(ctx, name)
			if err != nil {
				result.Errors[name] = err
			} else {
				result.Values[name] = sha
			}
			continue
		}
		batchNames = append(batchNames, name)
		offsets = append(offsets, len(queries))
		if !strings.HasPrefix(name, stackitRefPrefix) {
			queries = append(queries, name+"^{commit}")
		}
		queries = append(queries, name)
	}
	if len(queries) == 0 {
		return result
	}
	offsets = append(offsets, len(queries))
	out, err := r.runGitInternal(ctx, strings.Join(queries, "\n")+"\n",
		nil, false, "cat-file", "--batch-check=%(objectname)")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if err == nil && len(lines) != len(queries) {
		err = fmt.Errorf("batch cat-file returned %d lines for %d queries", len(lines), len(queries))
	}
	if err != nil {
		for _, name := range batchNames {
			result.Errors[name] = err
		}
		return result
	}
	for i, name := range batchNames {
		for _, sha := range lines[offsets[i]:offsets[i+1]] {
			if isHexSHA(sha) {
				result.Values[name] = sha
				break
			}
		}
		if _, ok := result.Values[name]; !ok {
			// rev-parse accepts full IDs whose objects are absent.
			if isHexSHA(strings.ToLower(name)) {
				if sha, err := r.resolveRefSHAContext(ctx, name); err == nil {
					result.Values[name] = sha
					continue
				}
			}
			result.Errors[name] = fmt.Errorf("failed to get revision for %s: reference not found", name)
		}
	}
	return result
}
