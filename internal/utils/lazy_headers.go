package utils

// LazyHeaders decides when a keyed section's header should be emitted to a
// streaming display: at most once, on the section's first item, and only for
// sections that were explicitly started. Successive emitted headers are
// separated from the previous one by a blank line.
//
// It holds no output of its own — callers emit based on its decisions — so the
// same logic drives both the non-TTY sync handler (which writes lines straight
// to output) and the bubbletea sync model (which accumulates tea.Cmds). Keeping
// the rule in one place stops the two from drifting apart.
//
// The zero value is not ready for use; construct with NewLazyHeaders.
type LazyHeaders[K comparable] struct {
	started   map[K]bool
	committed map[K]bool
	any       bool
}

// NewLazyHeaders returns a ready-to-use tracker keyed by K.
func NewLazyHeaders[K comparable]() *LazyHeaders[K] {
	return &LazyHeaders[K]{
		started:   make(map[K]bool),
		committed: make(map[K]bool),
	}
}

// Start records that a section has begun. Its header is not emitted yet — that
// happens lazily on the first item via CommitOnItem — so a section that starts
// but never produces an item stays silent.
func (l *LazyHeaders[K]) Start(key K) { l.started[key] = true }

// CommitOnItem is called as a section produces an item. It reports whether the
// section's header should be emitted now (emit) — true exactly once, for the
// first item of a started, not-yet-committed section — and whether that header
// needs a blank-line separator from the previous section (separate). Items for
// a section that was never started return (false, false), so callers that drive
// items without starting a phase stay headerless.
func (l *LazyHeaders[K]) CommitOnItem(key K) (emit, separate bool) {
	if !l.started[key] || l.committed[key] {
		return false, false
	}
	l.committed[key] = true
	separate = l.any
	l.any = true
	return true, separate
}

// Committed reports whether the given section's header has been emitted.
func (l *LazyHeaders[K]) Committed(key K) bool { return l.committed[key] }

// Any reports whether any header has been emitted, e.g. to decide whether a
// trailing separator is needed before a summary line.
func (l *LazyHeaders[K]) Any() bool { return l.any }
