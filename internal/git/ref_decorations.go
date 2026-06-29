package git

import (
	"context"
	"slices"
	"strings"
)

// RefDecoration is a single local ref (branch head or tag) pointing at a commit.
// It backs git-log-style "(main, tag: v1.4.0)" annotations.
type RefDecoration struct {
	Name  string // short ref name, e.g. "main" or "v1.4.0"
	IsTag bool   // true for refs/tags/*, false for refs/heads/*
}

// RefDecorations returns local branch and tag refs grouped by the full commit
// SHA they point at. Annotated tags are dereferenced to the commit they wrap
// (via for-each-ref's `*objectname`) so a tag decoration lands on the commit, not
// the intermediate tag object. Commits with no refs are simply absent from the map.
//
// Within each commit's slice the order is git-log-style and explicit: branch
// heads first, then tags, alphabetical by name within each group. We sort rather
// than lean on for-each-ref's refname order (which only happens to put "heads"
// before "tags" lexically) so the grouping survives reordered ref args or a new
// namespace like refs/remotes.
func (r *runner) RefDecorations() (map[string][]RefDecoration, error) {
	// %(*objectname) is empty for lightweight tags and branch heads, and the
	// wrapped commit SHA for annotated tags. Prefer it when present.
	out, err := r.RunGitCommandWithContext(context.Background(),
		"for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(*objectname)",
		"refs/heads", "refs/tags",
	)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]RefDecoration)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) < 2 {
			continue
		}
		refname, objectname := fields[0], fields[1]
		sha := objectname
		if len(fields) >= 3 && fields[2] != "" {
			sha = fields[2] // annotated tag: dereference to the wrapped commit
		}
		if sha == "" {
			continue
		}

		switch {
		case strings.HasPrefix(refname, "refs/tags/"):
			result[sha] = append(result[sha], RefDecoration{Name: strings.TrimPrefix(refname, "refs/tags/"), IsTag: true})
		case strings.HasPrefix(refname, "refs/heads/"):
			result[sha] = append(result[sha], RefDecoration{Name: strings.TrimPrefix(refname, "refs/heads/")})
		}
	}

	// Branch heads before tags, alphabetical within each group.
	for _, decos := range result {
		slices.SortFunc(decos, func(a, b RefDecoration) int {
			if a.IsTag != b.IsTag {
				if a.IsTag {
					return 1
				}
				return -1
			}
			return strings.Compare(a.Name, b.Name)
		})
	}
	return result, nil
}
