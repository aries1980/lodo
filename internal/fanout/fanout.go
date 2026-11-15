// Package fanout runs independent indexed work with a bounded number of
// goroutines. Results keep their index, so callers can assemble output in the
// original order after a concurrent read.
package fanout

import (
	"context"
	"errors"
	"sync"
)

// Do calls fn for every index in [0, n), running at most workers calls at
// once. fn is called concurrently and must not mutate shared state. Results
// are returned in index order. The first failure cancels the work still
// running; Do waits for every started call and returns the lowest-index
// failure that is not a cancellation caused by that failure. A cancelled
// parent context is returned as its own error. workers is clamped to [1, n];
// n <= 0 returns no results and no error.
func Do[T any](ctx context.Context, n, workers int, fn func(context.Context, int) (T, error)) ([]T, error) {
	if n <= 0 {
		return nil, nil
	}
	workers = min(max(workers, 1), n)
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := make([]T, n)
	errs := make([]error, n)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				value, err := fn(ctx, i)
				if err != nil {
					errs[i] = err
					cancel()
					return
				}
				out[i] = value
			}
		})
	}
	for i := range n {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()

	if err := parent.Err(); err != nil {
		return nil, err
	}
	var first error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if first == nil {
			first = err
		}
		if errors.Is(err, context.Canceled) {
			continue
		}
		return nil, err
	}
	if first != nil {
		return nil, first
	}
	return out, nil
}
