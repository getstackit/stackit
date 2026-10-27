package utils

import (
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
