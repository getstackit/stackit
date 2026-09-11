package git

import (
	"context"
	"fmt"
	"strings"
)

// BatchDiffNumstat compares independent ranges with one diff-tree process.
// It returns the same line-oriented numstat format as GetDiffNumstat. Callers
// may fall back to individual reads on error; no partial results are returned.
func (r *runner) BatchDiffNumstat(ctx context.Context, ranges []RevRange) (map[RevRange]string, error) {
	result := make(map[RevRange]string, len(ranges))
	if len(ranges) == 0 {
		return result, nil
	}

	refs := make([]string, 0, len(ranges)*2)
	seen := make(map[string]bool)
	for _, rr := range ranges {
		for _, ref := range []string{rr.Base, rr.Head} {
			if ref == "" {
				return nil, fmt.Errorf("batch diff requires explicit base and head")
			}
			if !seen[ref] {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}
	args := []string{"rev-parse", "--revs-only", "--end-of-options"}
	for _, ref := range refs {
		args = append(args, ref+"^{tree}")
	}
	out, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(out)
	if len(shas) != len(refs) {
		return nil, fmt.Errorf("batch diff resolved %d trees for %d refs", len(shas), len(refs))
	}
	trees := make(map[string]string, len(refs))
	for i, ref := range refs {
		trees[ref] = shas[i]
	}

	var input strings.Builder
	sections := make(map[string]*strings.Builder)
	keys := make(map[RevRange]string, len(ranges))
	for _, rr := range ranges {
		if trees[rr.Base] == trees[rr.Head] {
			result[rr] = ""
			continue
		}
		key := trees[rr.Base] + " " + trees[rr.Head]
		keys[rr] = key
		if sections[key] == nil {
			sections[key] = &strings.Builder{}
			input.WriteString(key + "\n")
		}
	}
	if len(sections) == 0 {
		return result, nil
	}
	options, err := r.batchDiffOptions(ctx)
	if err != nil {
		return nil, err
	}
	args = append([]string{"diff-tree", "--stdin", "-r", "--numstat"}, options...)
	out, err = r.runGitInternal(ctx, input.String(), nil, false, args...)
	if err != nil {
		return nil, err
	}
	var current *strings.Builder
	seenHeaders := make(map[string]bool, len(sections))
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if section := sections[line]; section != nil {
			current = section
			seenHeaders[line] = true
		} else if current != nil && strings.Contains(line, "\t") {
			current.WriteString(line + "\n")
		} else {
			return nil, fmt.Errorf("unexpected batch diff output %q", line)
		}
	}
	if len(seenHeaders) != len(sections) {
		return nil, fmt.Errorf("batch diff returned %d comparisons for %d ranges", len(seenHeaders), len(sections))
	}
	for rr, key := range keys {
		result[rr] = strings.TrimSpace(sections[key].String())
	}
	return result, nil
}

// diff-tree does not apply these porcelain-only settings itself. Pass them
// explicitly to retain git diff's rename/copy and submodule behavior.
func (r *runner) batchDiffOptions(ctx context.Context) ([]string, error) {
	const renamesEnabled = "true"
	out, err := r.RunGitCommandRawWithContext(ctx, "config", "--null", "--get-regexp", `^diff\.(renames|ignoresubmodules)$`)
	if err != nil && !isExitCode(err, 1) {
		return nil, err
	}
	renames := renamesEnabled
	ignoreSubmodules := ""
	for entry := range strings.SplitSeq(out, "\x00") {
		key, value, hasValue := strings.Cut(entry, "\n")
		switch key {
		case "diff.renames":
			renames = strings.ToLower(value)
			if !hasValue {
				renames = renamesEnabled
			}
		case "diff.ignoresubmodules":
			ignoreSubmodules = value
		}
	}
	var options []string
	switch renames {
	case renamesEnabled, "yes", "on", "1":
		options = append(options, "--find-renames")
	case "false", "no", "off", "0", "":
		options = append(options, "--no-renames")
	case "copy", "copies":
		options = append(options, "--find-copies")
	default:
		return nil, fmt.Errorf("unsupported diff.renames value %q", renames)
	}
	if ignoreSubmodules != "" {
		options = append(options, "--ignore-submodules="+ignoreSubmodules)
	}
	return options, nil
}
