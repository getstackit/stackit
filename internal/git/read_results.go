package git

import (
	"fmt"
	"slices"
)

// ReadResults holds the successful values and per-input failures of a batched
// read. Accessors only inspect these results; they never perform Git I/O.
type ReadResults[T any] struct {
	Values map[string]T
	Errors map[string]error
}

// Get returns the result for a requested input, including its read error.
func (r ReadResults[T]) Get(name string) (T, error) {
	if err := r.Errors[name]; err != nil {
		var zero T
		return zero, err
	}
	if value, ok := r.Values[name]; ok {
		return value, nil
	}
	var zero T
	return zero, fmt.Errorf("no read result for %s", name)
}

// One extracts a one-input read without repeating the input expression.
func (r ReadResults[T]) One() (T, error) {
	if len(r.Values)+len(r.Errors) != 1 {
		var zero T
		return zero, fmt.Errorf("expected one read result, got %d", len(r.Values)+len(r.Errors))
	}
	for name := range r.Errors {
		return r.Get(name)
	}
	for name := range r.Values {
		return r.Get(name)
	}
	panic("unreachable")
}

// Split returns the values and failures for callers that process them separately.
func (r ReadResults[T]) Split() (map[string]T, map[string]error) {
	return r.Values, r.Errors
}

// ValuesAndErrors returns values alongside a deterministic error summary.
func (r ReadResults[T]) ValuesAndErrors() (map[string]T, []error) {
	return r.Values, r.Errs()
}

// Errs returns failures in input-name order for callers reporting a summary.
func (r ReadResults[T]) Errs() []error {
	names := make([]string, 0, len(r.Errors))
	for name := range r.Errors {
		names = append(names, name)
	}
	slices.Sort(names)
	errs := make([]error, 0, len(names))
	for _, name := range names {
		errs = append(errs, r.Errors[name])
	}
	return errs
}
