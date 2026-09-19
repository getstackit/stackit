package git

import "fmt"

// One extracts the sole value from a one-input write. It only handles the
// returned values and error; it never performs I/O or repeats the write.
func One[T any](values []T, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	if len(values) != 1 {
		return zero, fmt.Errorf("expected one write result, got %d", len(values))
	}
	return values[0], nil
}
