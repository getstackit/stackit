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
	result := ReadResults[CommitMetadata]{}
	queries := make([]string, len(refs))
	for i, ref := range refs {
		queries[i] = ref + "^{commit}"
	}
	revs := r.ReadRevisions(ctx, queries...)
	shas := make([]string, 0, len(refs))
	for _, outcome := range revs.All() {
		if outcome.Err == nil {
			shas = append(shas, outcome.Value)
		}
	}
	commits, readErr := readCommitTips(r, ctx, metadataCodec, shas)
	for _, ref := range refs {
		sha, err := revs.Get(ref + "^{commit}")
		if err == nil {
			err = readErr
		}
		if err != nil {
			result.Fail(ref, err)
		} else if commit, ok := commits[sha]; ok {
			result.Set(ref, commit)
		} else {
			result.Fail(ref, fmt.Errorf("commit not found: %s", ref))
		}
	}
	return result
}

func readCommitTips[T commitRecord](r *runner, ctx context.Context, codec commitCodec[T], shas []string) (map[string]T, error) {
	result := make(map[string]T, len(shas))
	if len(shas) == 0 {
		return result, nil
	}
	out, err := r.runGitInternal(ctx, strings.Join(shas, "\n")+"\n", nil, false,
		"log", "--no-walk=unsorted", "--stdin", "-z", "--format="+codec.format)
	if err != nil {
		return nil, err
	}
	commits, err := parseCommitRecords(out, codec)
	if err != nil {
		return nil, err
	}
	for _, commit := range commits {
		result[commit.node().SHA] = commit
	}
	return result, nil
}

// ReadCommitRanges returns newest-first commits keyed by RevRange.String().
// Linear ranges share tip reads, including overlapping ranges. This costs one
// resolution plus one read per uncached generation, not one log per branch.
// Merge, unbounded, and unusually deep ranges use Git's native walk to preserve
// exact reachability and ordering. No ambient revision or graph cache is kept.
func (r *runner) ReadCommitRanges(ctx context.Context, ranges ...RevRange) ReadResults[[]CommitMetadata] {
	return readWalkResults(walkCommitRanges(r, ctx, metadataCodec, ranges...), func(commits []CommitMetadata) []CommitMetadata {
		return commits
	}, func(rr RevRange) ([]CommitMetadata, error) { return readCommitRange(r, ctx, metadataCodec, rr) })
}

// ReadCommitNodes reads topology without allocating display/replay fields.
func (r *runner) ReadCommitNodes(ctx context.Context, ranges ...RevRange) ReadResults[[]CommitNode] {
	return readWalkResults(walkCommitRanges(r, ctx, nodeCodec, ranges...), func(nodes []CommitNode) []CommitNode {
		return nodes
	}, func(rr RevRange) ([]CommitNode, error) { return readCommitRange(r, ctx, nodeCodec, rr) })
}

// ReadCommitCounts uses a shared walk for short linear histories and Git's
// count-only traversal for native fallbacks.
func (r *runner) ReadCommitCounts(ctx context.Context, ranges ...RevRange) ReadResults[int] {
	return readWalkResults(walkCommitRanges(r, ctx, nodeCodec, ranges...), func(commits []CommitNode) int {
		return len(commits)
	}, func(rr RevRange) (int, error) {
		rangeArg := rr.Head
		if rr.Base != "" {
			rangeArg = rr.String()
		}
		out, err := r.RunGitCommandWithContext(ctx, "rev-list", "--count", "--end-of-options", rangeArg, "--")
		if err != nil {
			return 0, err
		}
		return strconv.Atoi(out)
	})
}

// ReadAncestry proves ancestry with a complete linear walk or delegates to Git.
func (r *runner) ReadAncestry(ctx context.Context, ranges ...RevRange) ReadResults[bool] {
	return readWalkResults(walkCommitRanges(r, ctx, nodeCodec, ranges...), func([]CommitNode) bool {
		return true
	}, func(rr RevRange) (bool, error) { return r.IsAncestor(ctx, rr.Base, rr.Head) })
}

// A walk either reaches its base, or needs Git's native reachability semantics.
// Native is explicit: an empty completed walk is still a successful result.
type commitWalk[T commitRecord] struct {
	Commits []T
	Native  *RevRange
}

func readWalkResults[C commitRecord, T any](walks ReadResults[commitWalk[C]], linear func([]C) T, native func(RevRange) (T, error)) ReadResults[T] {
	var result ReadResults[T]
	for key, outcome := range walks.All() {
		if outcome.Err != nil {
			result.Fail(key, outcome.Err)
			continue
		}
		walk := outcome.Value
		value := linear(walk.Commits)
		var err error
		if walk.Native != nil {
			value, err = native(*walk.Native)
		}
		result.Record(key, value, err)
	}
	return result
}

func walkCommitRanges[T commitRecord](r *runner, ctx context.Context, codec commitCodec[T], ranges ...RevRange) ReadResults[commitWalk[T]] {
	result := ReadResults[commitWalk[T]]{}
	readNative := func(key string, rr RevRange) {
		result.Set(key, commitWalk[T]{Native: &rr})
	}
	if len(ranges) == 1 {
		readNative(ranges[0].String(), ranges[0])
		return result
	}
	type walk struct {
		key      string
		rangeSHA RevRange
		next     string
		commits  []T
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
			result.Fail(key, err)
			continue
		}
		resolved := RevRange{Base: base, Head: head}
		switch base {
		case "":
			readNative(key, resolved)
		case head:
			result.Set(key, commitWalk[T]{})
		default:
			pending = append(pending, walk{key: key, rangeSHA: resolved, next: head})
		}
	}
	known := make(map[string]T)
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
		commits, err := readCommitTips(r, ctx, codec, frontier)
		if err != nil {
			for _, w := range pending {
				result.Fail(w.key, err)
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
					result.Set(w.key, commitWalk[T]{Commits: w.commits})
					break
				}
				commit, ok := known[w.next]
				if !ok {
					if requested[w.next] {
						result.Fail(w.key, fmt.Errorf("commit not returned: %s", w.next))
					} else {
						next = append(next, w)
					}
					break
				}
				if len(commit.node().Parents) != 1 {
					readNative(w.key, w.rangeSHA)
					break
				}
				w.commits = append(w.commits, commit)
				w.next = commit.node().Parents[0]
			}
		}
		pending = next
	}
	for _, w := range pending {
		readNative(w.key, w.rangeSHA)
	}
	return result
}
