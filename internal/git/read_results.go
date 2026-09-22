package git

import (
	"fmt"
	"iter"
	"maps"
	"slices"
)

// ReadResult represents one observed input, including successful empty values.
type ReadResult[T any] struct {
	Value T
	Err   error
}

// ReadResults has one authoritative entry per input. An absent entry means the
// input was not read, rather than an empty success. Its zero value is ready to use.
type ReadResults[T any] struct {
	entries map[string]ReadResult[T]
}

// Record replaces an input's outcome, so a failure cannot coexist with a success.
func (r *ReadResults[T]) Record(name string, value T, err error) {
	if r.entries == nil {
		r.entries = make(map[string]ReadResult[T])
	}
	if err != nil {
		var zero T
		value = zero
	}
	r.entries[name] = ReadResult[T]{Value: value, Err: err}
}

// Set records a successful read, including a zero or nil value.
func (r *ReadResults[T]) Set(name string, value T) { r.Record(name, value, nil) }

// Fail records a failed read, replacing any earlier value.
func (r *ReadResults[T]) Fail(name string, err error) {
	var zero T
	r.Record(name, zero, err)
}

// All iterates the recorded outcomes without allocating separate value/error maps.
func (r ReadResults[T]) All() iter.Seq2[string, ReadResult[T]] {
	return maps.All(r.entries)
}

// Get returns the result for a requested input, including its read error.
func (r ReadResults[T]) Get(name string) (T, error) {
	if result, ok := r.entries[name]; ok {
		return result.Value, result.Err
	}
	var zero T
	return zero, fmt.Errorf("no read result for %s", name)
}

// One extracts a one-input read without repeating the input expression.
func (r ReadResults[T]) One() (T, error) {
	if len(r.entries) != 1 {
		var zero T
		return zero, fmt.Errorf("expected one read result, got %d", len(r.entries))
	}
	for _, result := range r.entries {
		return result.Value, result.Err
	}
	panic("unreachable")
}

// Split returns independent maps for callers that process successes and failures separately.
func (r ReadResults[T]) Split() (map[string]T, map[string]error) {
	return r.Values(), r.Failures()
}

// Values returns a copy of the successful results.
func (r ReadResults[T]) Values() map[string]T {
	values := make(map[string]T)
	for name, result := range r.entries {
		if result.Err == nil {
			values[name] = result.Value
		}
	}
	return values
}

// Failures returns a copy of the per-input errors.
func (r ReadResults[T]) Failures() map[string]error {
	errors := make(map[string]error)
	for name, result := range r.entries {
		if result.Err != nil {
			errors[name] = result.Err
		}
	}
	return errors
}

// ValuesAndErrors returns values alongside a deterministic error summary.
func (r ReadResults[T]) ValuesAndErrors() (map[string]T, []error) {
	return r.Values(), r.Errs()
}

// Errs returns failures in input-name order for callers reporting a summary.
func (r ReadResults[T]) Errs() []error {
	errors := r.Failures()
	names := slices.Sorted(maps.Keys(errors))
	errs := make([]error, 0, len(names))
	for _, name := range names {
		errs = append(errs, errors[name])
	}
	return errs
}
