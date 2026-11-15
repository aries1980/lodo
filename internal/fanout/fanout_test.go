package fanout_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aries1980/lodo/internal/fanout"
)

func TestDoKeepsIndexOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = 8
		got, err := fanout.Do(t.Context(), n, 3, func(_ context.Context, i int) (int, error) {
			time.Sleep(time.Duration(n-i) * time.Millisecond)
			return i * 10, nil
		})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if len(got) != n {
			t.Fatalf("results = %d, want %d", len(got), n)
		}
		for i, value := range got {
			if value != i*10 {
				t.Fatalf("result %d = %d, want %d", i, value, i*10)
			}
		}
	})
}

func TestDoBoundsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n, workers = 20, 3
		var inFlight, peak int32
		started := make(chan struct{}, n)
		release := make(chan struct{})
		done := make(chan error, 1)

		go func() {
			_, err := fanout.Do(t.Context(), n, workers, func(_ context.Context, i int) (int, error) {
				current := atomic.AddInt32(&inFlight, 1)
				for {
					seen := atomic.LoadInt32(&peak)
					if current <= seen || atomic.CompareAndSwapInt32(&peak, seen, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				atomic.AddInt32(&inFlight, -1)
				return i, nil
			})
			done <- err
		}()

		synctest.Wait()
		if got := len(started); got != workers {
			t.Fatalf("started = %d, want %d workers", got, workers)
		}
		if got := atomic.LoadInt32(&peak); got != workers {
			t.Fatalf("peak in flight = %d, want %d", got, workers)
		}

		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatalf("Do: %v", err)
		}
		if got := atomic.LoadInt32(&peak); got > workers {
			t.Fatalf("peak in flight = %d, want at most %d", got, workers)
		}
	})
}

func TestDoCancelsOnFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		boom := errors.New("boom")
		var calls int32
		_, err := fanout.Do(t.Context(), 20, 3, func(ctx context.Context, i int) (int, error) {
			atomic.AddInt32(&calls, 1)
			if i == 0 {
				return 0, boom
			}
			<-ctx.Done()
			return 0, ctx.Err()
		})
		if !errors.Is(err, boom) {
			t.Fatalf("Do err = %v, want the first failure", err)
		}
		if got := atomic.LoadInt32(&calls); got >= 20 {
			t.Fatalf("calls = %d, want cancellation to stop the rest", got)
		}
	})
}

func TestDoParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		started := make(chan struct{}, 5)
		done := make(chan error, 1)

		go func() {
			_, err := fanout.Do(ctx, 5, 5, func(ctx context.Context, _ int) (int, error) {
				started <- struct{}{}
				<-ctx.Done()
				return 0, ctx.Err()
			})
			done <- err
		}()

		synctest.Wait()
		if got := len(started); got != 5 {
			t.Fatalf("started = %d, want 5", got)
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("Do err = %v, want context.Canceled", err)
		}
	})
}

func TestDoZeroWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got, err := fanout.Do(t.Context(), 0, 4, func(context.Context, int) (int, error) {
			t.Error("fn called for zero work")
			return 0, nil
		})
		if err != nil || got != nil {
			t.Fatalf("Do = %v, %v; want nil, nil", got, err)
		}
	})
}

func TestDoClampsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got, err := fanout.Do(t.Context(), 2, 99, func(_ context.Context, i int) (int, error) {
			return i, nil
		})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if len(got) != 2 || got[0] != 0 || got[1] != 1 {
			t.Fatalf("Do = %v, want [0 1]", got)
		}
	})
}
