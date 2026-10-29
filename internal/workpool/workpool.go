// Package workpool runs work over a slice on a bounded pool of goroutines.
package workpool

import (
	"errors"
	"runtime"
	"sync"
)

// Run runs the given worker function for each item in the slice in parallel.
// It uses runtime.GOMAXPROCS(0) as the default number of workers.
func Run[T any](items []T, worker func(item T)) {
	RunN(items, 0, worker)
}

// RunN is Run with an explicit worker count; numWorkers <= 0 means GOMAXPROCS.
func RunN[T any](items []T, numWorkers int, worker func(item T)) {
	if len(items) == 0 {
		return
	}

	if numWorkers <= 0 {
		numWorkers = runtime.GOMAXPROCS(0)
	}
	numWorkers = min(numWorkers, len(items))

	jobs := make(chan T, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for range numWorkers {
		go func() {
			defer wg.Done()
			for item := range jobs {
				worker(item)
			}
		}()
	}
	wg.Wait()
}

// RunAll runs worker for each item on a pool of numWorkers goroutines (0 means
// GOMAXPROCS) and returns every worker error joined. A failing item does not
// stop the others, so callers see all failures, not just the first. With one
// worker, items run in slice order.
func RunAll[T any](items []T, numWorkers int, worker func(item T) error) error {
	var mu sync.Mutex
	var errs []error
	RunN(items, numWorkers, func(item T) {
		if err := worker(item); err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	})
	return errors.Join(errs...)
}
