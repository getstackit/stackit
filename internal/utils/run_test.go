package utils

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3, 4, 5}
	var sum, calls atomic.Int64

	Run(items, func(item int) {
		sum.Add(int64(item))
		calls.Add(1)
	})

	if calls.Load() != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls.Load(), len(items))
	}
	if sum.Load() != 15 {
		t.Fatalf("sum = %d, want 15", sum.Load())
	}
}

func TestRunEmpty(t *testing.T) {
	t.Parallel()

	Run([]int{}, func(int) {
		t.Fatal("worker should not be called for empty input")
	})
}

func TestRunWithWorkers(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3, 4, 5, 6, 7, 8}
	var calls atomic.Int64

	RunWithWorkers(items, 3, func(int) {
		calls.Add(1)
	})

	if calls.Load() != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls.Load(), len(items))
	}
}

func TestRunWithWorkersMoreWorkersThanItems(t *testing.T) {
	t.Parallel()

	items := []int{1, 2}
	var calls atomic.Int64

	RunWithWorkers(items, 10, func(int) {
		calls.Add(1)
	})

	if calls.Load() != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls.Load(), len(items))
	}
}

func TestRunAllJoinsEveryError(t *testing.T) {
	t.Parallel()

	errOdd := errors.New("odd")
	var calls atomic.Int64

	err := RunAll([]int{1, 2, 3, 4, 5}, 0, func(item int) error {
		calls.Add(1)
		if item%2 == 1 {
			return fmt.Errorf("item %d: %w", item, errOdd)
		}
		return nil
	})

	if calls.Load() != 5 {
		t.Fatalf("calls = %d, want 5: a failure must not stop the other items", calls.Load())
	}
	if !errors.Is(err, errOdd) {
		t.Fatalf("err = %v, want it to wrap errOdd", err)
	}
	if got := len(err.(interface{ Unwrap() []error }).Unwrap()); got != 3 {
		t.Fatalf("joined %d errors, want 3", got)
	}
}

func TestRunAllSingleWorkerKeepsOrder(t *testing.T) {
	t.Parallel()

	var order []int
	err := RunAll([]int{1, 2, 3, 4}, 1, func(item int) error {
		order = append(order, item)
		return nil
	})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !slices.Equal(order, []int{1, 2, 3, 4}) {
		t.Fatalf("order = %v, want input order", order)
	}
}
