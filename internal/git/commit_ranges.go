package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ReadCommits reads display/replay data together, keyed by the requested ref.
// Resolution failures remain associated with their input rather than poisoning
// unrelated commits in the batch.
func (r *runner) ReadCommits(ctx context.Context, refs ...string) ReadResults[CommitMetadata] {
	result := ReadResults[CommitMetadata]{Values: make(map[string]CommitMetadata), Errors: make(map[string]error)}
	queries := make([]string, len(refs))
	for i, ref := range refs {
		queries[i] = ref + "^{commit}"
	}
	revs := r.ReadRevisions(ctx, queries...)
	shas := make([]string, 0, len(revs.Values))
	for _, sha := range revs.Values {
		shas = append(shas, sha)
	}
	commits, readErr := r.readCommitTips(ctx, CommitDetails, shas)
	for _, ref := range refs {
		sha, err := revs.Get(ref + "^{commit}")
		if err == nil {
			err = readErr
		}
		if err != nil {
			result.Errors[ref] = err
		} else if commit, ok := commits[sha]; ok {
			result.Values[ref] = commit
		} else {
			result.Errors[ref] = fmt.Errorf("commit not found: %s", ref)
		}
	}
	return result
}

func (r *runner) readCommitTips(ctx context.Context, mode CommitReadMode, shas []string) (map[string]CommitMetadata, error) {
	result := make(map[string]CommitMetadata, len(shas))
	if len(shas) == 0 {
		return result, nil
	}
	out, err := r.runGitInternal(ctx, strings.Join(shas, "\n")+"\n", nil, false,
		"log", "--no-walk=unsorted", "--stdin", "-z", "--format="+commitReadFormat(mode))
	if err != nil {
		return nil, err
	}
	commits, err := parseCommitRecords(out, mode)
	if err != nil {
		return nil, err
	}
	for _, commit := range commits {
		result[commit.SHA] = commit
	}
	return result, nil
}

// ReadCommitRanges returns newest-first commits keyed by RevRange.String().
// Linear ranges share tip reads, including overlapping ranges. This costs one
// resolution plus one read per uncached generation, not one log per branch.
// Merge, unbounded, and unusually deep ranges use Git's native walk to preserve
// exact reachability and ordering. No ambient revision or graph cache is kept.
func (r *runner) ReadCommitRanges(ctx context.Context, mode CommitReadMode, ranges ...RevRange) ReadResults[[]CommitMetadata] {
	return r.readCommitRanges(ctx, mode, commitWalkResults{}, ranges...)
}

// ReadCommitCounts shares linear walks but uses rev-list --count for native
// fallbacks, avoiding materializing arbitrarily large histories just to count.
func (r *runner) ReadCommitCounts(ctx context.Context, ranges ...RevRange) ReadResults[int] {
	values := make(map[string]int)
	data := r.readCommitRanges(ctx, CommitIDs, commitWalkResults{counts: values}, ranges...)
	return ReadResults[int]{Values: values, Errors: data.Errors}
}

// ReadAncestry checks whether each range's base is an ancestor of its head.
// Linear histories share the same bounded walk as commit ranges; ambiguous
// histories fall back to merge-base --is-ancestor, never to a guessed answer.
func (r *runner) ReadAncestry(ctx context.Context, ranges ...RevRange) ReadResults[bool] {
	values := make(map[string]bool)
	data := r.readCommitRanges(ctx, CommitIDs, commitWalkResults{ancestry: values}, ranges...)
	return ReadResults[bool]{Values: values, Errors: data.Errors}
}

// Optional projections of a shared walk. Exactly one projection is used per
// operation; native fallbacks use the corresponding specialized Git command.
type commitWalkResults struct {
	ancestry map[string]bool
	counts   map[string]int
}

func (r *runner) readCommitRanges(ctx context.Context, mode CommitReadMode, projected commitWalkResults, ranges ...RevRange) ReadResults[[]CommitMetadata] {
	result := ReadResults[[]CommitMetadata]{Values: make(map[string][]CommitMetadata), Errors: make(map[string]error)}
	if mode != CommitIDs && mode != CommitDetails {
		for _, rr := range ranges {
			result.Errors[rr.String()] = fmt.Errorf("invalid commit read mode %d", mode)
		}
		return result
	}
	readNative := func(key string, rr RevRange) {
		if projected.counts != nil {
			rangeArg := rr.Head
			if rr.Base != "" {
				rangeArg = rr.String()
			}
			out, err := r.RunGitCommandWithContext(ctx, "rev-list", "--count", "--end-of-options", rangeArg, "--")
			if err == nil {
				var count int
				count, err = strconv.Atoi(out)
				if err == nil {
					projected.counts[key] = count
				}
			}
			if err != nil {
				result.Errors[key] = err
			}
			return
		}
		if projected.ancestry != nil {
			value, err := r.IsAncestor(ctx, rr.Base, rr.Head)
			if err != nil {
				result.Errors[key] = err
			} else {
				projected.ancestry[key] = value
			}
			return
		}
		commits, err := r.readCommitRange(ctx, mode, rr)
		if err != nil {
			result.Errors[key] = err
		} else {
			result.Values[key] = commits
		}
	}
	if len(ranges) == 1 {
		readNative(ranges[0].String(), ranges[0])
		return result
	}
	type walk struct {
		key      string
		rangeSHA RevRange
		next     string
		commits  []CommitMetadata
	}
	refs := make([]string, 0, 2*len(ranges))
	for _, rr := range ranges {
		refs = append(refs, rr.Head+"^{commit}")
		if rr.Base != "" {
			refs = append(refs, rr.Base+"^{commit}")
		}
	}
	revs := r.ReadRevisions(ctx, refs...)
	pending := make([]walk, 0, len(ranges))
	seen := make(map[string]bool)
	for _, rr := range ranges {
		key := rr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		head, err := revs.Get(rr.Head + "^{commit}")
		base := ""
		if err == nil && rr.Base != "" {
			base, err = revs.Get(rr.Base + "^{commit}")
		}
		if err != nil {
			result.Errors[key] = err
			continue
		}
		resolved := RevRange{Base: base, Head: head}
		switch base {
		case "":
			readNative(key, resolved)
		case head:
			result.Values[key] = nil
			if projected.ancestry != nil {
				projected.ancestry[key] = true
			}
			if projected.counts != nil {
				projected.counts[key] = 0
			}
		default:
			pending = append(pending, walk{key: key, rangeSHA: resolved, next: head})
		}
	}
	known := make(map[string]CommitMetadata)
	// Bound speculative reads when a base is unrelated or far down history.
	// The common case (many branches with a few commits each) finishes early.
	const maxGenerations = 8
	for generation := 0; len(pending) > 0 && generation < maxGenerations; generation++ {
		frontier := make([]string, 0, len(pending))
		requested := make(map[string]bool)
		for _, w := range pending {
			if _, ok := known[w.next]; !ok && !requested[w.next] {
				frontier = append(frontier, w.next)
				requested[w.next] = true
			}
		}
		commits, err := r.readCommitTips(ctx, mode, frontier)
		if err != nil {
			for _, w := range pending {
				result.Errors[w.key] = err
			}
			return result
		}
		for sha, commit := range commits {
			known[sha] = commit
		}
		next := make([]walk, 0, len(pending))
		for _, w := range pending {
			for {
				if w.next == w.rangeSHA.Base {
					result.Values[w.key] = w.commits
					if projected.ancestry != nil {
						projected.ancestry[w.key] = true
					}
					if projected.counts != nil {
						projected.counts[w.key] = len(w.commits)
					}
					break
				}
				commit, ok := known[w.next]
				if !ok {
					if requested[w.next] {
						result.Errors[w.key] = fmt.Errorf("commit not returned: %s", w.next)
					} else {
						next = append(next, w)
					}
					break
				}
				if len(commit.Parents) != 1 {
					readNative(w.key, w.rangeSHA)
					break
				}
				w.commits = append(w.commits, commit)
				w.next = commit.Parents[0]
			}
		}
		pending = next
	}
	for _, w := range pending {
		readNative(w.key, w.rangeSHA)
	}
	return result
}
