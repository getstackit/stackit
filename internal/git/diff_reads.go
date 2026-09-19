package git

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// DiffReadMode selects the work required by a diff consumer.
type DiffReadMode int

const (
	// DiffCheckOnly compares trees without walking files or reading blobs.
	DiffCheckOnly DiffReadMode = iota
	// DiffNames reads changed paths without computing line counts.
	DiffNames
	// DiffStats reads changed paths and additions/deletions (binary files count as zero lines).
	DiffStats
)

// DiffSummary describes a comparison between two trees. Files are sorted and
// renames use the destination path, matching git diff --name-only.
type DiffSummary struct {
	Empty   bool
	Files   []string
	Added   int
	Deleted int
}

// ReadDiffs resolves all trees together, then compares distinct tree pairs in
// one diff-tree process. Results (including individual resolution failures) are
// keyed by RevRange.String(). Empty bases are invalid: these are two-tree diffs,
// not history walks. Single-input callers use One(), exactly like ReadRevisions.
func (r *runner) ReadDiffs(ctx context.Context, mode DiffReadMode, ranges ...RevRange) ReadResults[DiffSummary] {
	result := ReadResults[DiffSummary]{Values: make(map[string]DiffSummary), Errors: make(map[string]error)}
	// A single file/stat request can let diff resolve its two revisions itself.
	// Batching adds a tree-resolution process, worthwhile only for multiple pairs.
	if len(ranges) == 1 && (mode == DiffNames || mode == DiffStats) && ranges[0].Base != "" && ranges[0].Head != "" {
		rr := ranges[0]
		format := "--raw"
		if mode == DiffStats {
			format = "--numstat"
		}
		out, err := r.RunGitCommandRawWithContext(ctx, "diff", "-z", "-M", "--no-ext-diff", "--no-textconv", format,
			"--end-of-options", rr.Base, rr.Head, "--")
		if err != nil {
			result.Errors[rr.String()] = err
			return result
		}
		summaries, err := parseDiffSummaries("single\n"+out, []string{"single"}, mode)
		if err != nil {
			result.Errors[rr.String()] = err
		} else {
			summary := summaries["single"]
			summary.Empty = out == ""
			result.Values[rr.String()] = summary
		}
		return result
	}
	refs := make([]string, 0, len(ranges)*2)
	for _, rr := range ranges {
		if mode < DiffCheckOnly || mode > DiffStats || rr.Base == "" || rr.Head == "" {
			result.Errors[rr.String()] = fmt.Errorf("invalid diff request: %s (mode %d)", rr, mode)
			continue
		}
		refs = append(refs, rr.Base+"^{tree}", rr.Head+"^{tree}")
	}
	trees := r.ReadRevisions(ctx, refs...)
	pairs := make([]string, 0, len(ranges))
	names := make(map[string][]string)
	seen := make(map[string]bool)
	for _, rr := range ranges {
		key := rr.String()
		if seen[key] || result.Errors[key] != nil {
			continue
		}
		seen[key] = true
		base, err := trees.Get(rr.Base + "^{tree}")
		if err != nil {
			result.Errors[key] = err
			continue
		}
		head, err := trees.Get(rr.Head + "^{tree}")
		if err != nil {
			result.Errors[key] = err
			continue
		}
		if base == head || mode == DiffCheckOnly {
			result.Values[key] = DiffSummary{Empty: base == head, Files: []string{}}
			continue
		}
		pair := base + " " + head
		if _, ok := names[pair]; !ok {
			pairs = append(pairs, pair)
		}
		names[pair] = append(names[pair], key)
	}
	if len(pairs) == 0 {
		return result
	}
	format := "--raw"
	if mode == DiffStats {
		format = "--numstat"
	}
	// Explicit rename detection matches porcelain diff's default, whereas
	// diff-tree defaults to no detection. No textconv/external helpers execute.
	out, err := r.runGitInternal(ctx, strings.Join(pairs, "\n")+"\n", nil, false,
		"diff-tree", "--stdin", "-r", "-z", "-M", "--no-ext-diff", "--no-textconv", format)
	var summaries map[string]DiffSummary
	if err == nil {
		summaries, err = parseDiffSummaries(out, pairs, mode)
	}
	for pair, keys := range names {
		for _, key := range keys {
			if err != nil {
				result.Errors[key] = err
			} else {
				result.Values[key] = summaries[pair]
			}
		}
	}
	return result
}

// Tree-pair headers end in LF; records and paths end in NUL. Consume paths
// explicitly so even a filename that looks like a header cannot split a batch.
func parseDiffSummaries(out string, pairs []string, mode DiffReadMode) (map[string]DiffSummary, error) {
	result := make(map[string]DiffSummary, len(pairs))
	for index, pair := range pairs {
		var ok bool
		out, ok = strings.CutPrefix(out, pair+"\n")
		if !ok {
			return nil, fmt.Errorf("missing diff-tree header for %s", pair)
		}
		summary := DiffSummary{Files: []string{}}
		for out != "" {
			if index+1 < len(pairs) && strings.HasPrefix(out, pairs[index+1]+"\n") {
				break
			}
			var record string
			record, out, ok = strings.Cut(out, "\x00")
			if !ok {
				return nil, fmt.Errorf("unterminated diff-tree record")
			}
			var path string
			switch mode {
			case DiffNames:
				fields := strings.Fields(record)
				if len(fields) != 5 || !strings.HasPrefix(record, ":") {
					return nil, fmt.Errorf("invalid raw diff record: %q", record)
				}
				path, out, ok = strings.Cut(out, "\x00")
				if ok && strings.HasPrefix(fields[4], "R") {
					path, out, ok = strings.Cut(out, "\x00")
				}
			case DiffStats:
				fields := strings.SplitN(record, "\t", 3)
				if len(fields) != 3 {
					return nil, fmt.Errorf("invalid numstat record: %q", record)
				}
				if fields[0] != "-" || fields[1] != "-" {
					added, addErr := strconv.Atoi(fields[0])
					deleted, delErr := strconv.Atoi(fields[1])
					if addErr != nil || delErr != nil || added < 0 || deleted < 0 {
						return nil, fmt.Errorf("invalid numstat counts: %q", record)
					}
					summary.Added += added
					summary.Deleted += deleted
				}
				path = fields[2]
				if path == "" { // Rename: empty path, then old and new NUL-terminated names.
					_, out, ok = strings.Cut(out, "\x00")
					if ok {
						path, out, ok = strings.Cut(out, "\x00")
					}
				}
			case DiffCheckOnly:
				return nil, fmt.Errorf("tree-only reads cannot contain diff records")
			}
			if !ok || path == "" {
				return nil, fmt.Errorf("missing diff-tree path")
			}
			summary.Files = append(summary.Files, path)
		}
		slices.Sort(summary.Files)
		result[pair] = summary
	}
	return result, nil
}
