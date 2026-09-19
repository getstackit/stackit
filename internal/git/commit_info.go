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

// CommitInfo holds a branch tip commit's author date and author name.
type CommitInfo struct {
	Date   time.Time
	Author string
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
