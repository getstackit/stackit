package split

import (
	"context"
	"fmt"
	"strings"
)

type splitStashEngine interface {
	StashList(context.Context) (string, error)
	StashPopRef(context.Context, string) error
}

// splitStashRef returns the exact ref created for this split operation. A
// split must never fall back to stash@{0}: users may create another stash
// while recovering from an error, and popping that stash loses their work.
func splitStashRef(ctx context.Context, eng splitStashEngine, marker string) (string, error) {
	list, err := eng.StashList(ctx)
	if err != nil {
		return "", fmt.Errorf("list stashes: %w", err)
	}
	for line := range strings.SplitSeq(list, "\n") {
		ref, message, ok := strings.Cut(line, ":")
		if ok && strings.Contains(message, marker) {
			return strings.TrimSpace(ref), nil
		}
	}
	return "", fmt.Errorf("split stash %q was not found", marker)
}
