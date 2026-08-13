package utils

import (
	"sync/atomic"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	items := []int{1, 2, 3, 4, 5}
	var sum int64
	var calls int64

	Run(items, func(item int) {
		atomic.AddInt64(&sum, int64(item))
		atomic.AddInt64(&calls, 1)
	})

	if calls != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls, len(items))
	}
	if sum != 15 {
		t.Fatalf("sum = %d, want 15", sum)
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
	var calls int64

	RunWithWorkers(items, 3, func(int) {
		atomic.AddInt64(&calls, 1)
	})

	if calls != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls, len(items))
	}
}

func TestRunWithWorkersMoreWorkersThanItems(t *testing.T) {
	t.Parallel()

	items := []int{1, 2}
	var calls int64

	RunWithWorkers(items, 10, func(int) {
		atomic.AddInt64(&calls, 1)
	})

	if calls != int64(len(items)) {
		t.Fatalf("calls = %d, want %d", calls, len(items))
	}
}
