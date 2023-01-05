// Package worker defines abstractions for parallelizing tasks.
package worker

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// ErrStopped is the error returned when the worker is stopped.
var ErrStopped = fmt.Errorf("worker is stopped")

// Void is a convenience struct for workers that do not actually return values.
type Void struct{}

// WorkFunc is a function for executing work.
type WorkFunc[T any] func() (T, error)

// Worker represents an instance of a worker. It is same for concurrent use, but
// see function documentation for more specific semantics.
type Worker[T any] struct {
	size int64
	sem  *semaphore.Weighted

	i           int64
	results     []*result[T]
	resultsLock sync.Mutex

	stopped uint32
}

// result is the internal result representation. It is primarily used to
// maintain results ordering.
type result[T any] struct {
	idx    int64
	result *Result[T]
}

// Result is the final result returned to the caller.
type Result[T any] struct {
	Value T
	Error error
}

// New creates a new worker that executes work in parallel, up to the maximum
// provided concurrency. Work is guaranteed to be executed in the order in which
// it was enqueued, but is not guaranteed to complete in the order in which it
// was enqueued (i.e. this is not a pipeline).
//
// If the provided concurrency is less than 1, it defaults to the number of CPU
// cores.
func New[T any](concurrency int64) *Worker[T] {
	if concurrency < 1 {
		concurrency = int64(runtime.NumCPU())
	}
	if concurrency < 1 {
		concurrency = 1
	}

	return &Worker[T]{
		size:    concurrency,
		i:       -1,
		sem:     semaphore.NewWeighted(concurrency),
		results: make([]*result[T], 0, concurrency),
	}
}

// Do adds new work into the queue. If there are no available workers, it blocks
// until a worker becomes available or until the provided context is cancelled.
// The function returns when the work has been successfully scheduled.
//
// To wait for all work to be completed and read the results, call
// [worker.Done]. This function only returns an error on two conditions:
//
//   - The worker was stopped via a call to [worker.Done]. You should not
//     enqueue more work. The error will be [ErrStopped].
//   - The incoming context was cancelled. You should probably not enqueue more
//     work, but this is an application-specific decision. The error will be
//     [context.DeadlineExceeded] or [context.Canceled].
//
// Never call Do from within a Do function because it will deadlock.
func (w *Worker[T]) Do(ctx context.Context, fn WorkFunc[T]) error {
	// Do not enqueue new work if the worker is stopped.
	if w.isStopped() {
		return ErrStopped
	}

	if err := w.sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to execute job: %w", err)
	}

	// It's possible the worker was stopped while we were waiting for the
	// semaphore to acquire, but the worker is actually stopped.
	if w.isStopped() {
		defer w.sem.Release(1)
		return ErrStopped
	}

	i := atomic.AddInt64(&w.i, 1)

	go func() {
		defer w.sem.Release(1)
		t, err := fn()

		w.resultsLock.Lock()
		defer w.resultsLock.Unlock()
		w.results = append(w.results, &result[T]{
			idx: i,
			result: &Result[T]{
				Value: t,
				Error: err,
			},
		})
	}()

	return nil
}

// Wait blocks until all queued jobs are finished.
func (w *Worker[T]) Wait(ctx context.Context) error {
	// Do not enqueue new work if the worker is stopped.
	if w.isStopped() {
		return ErrStopped
	}

	if err := w.sem.Acquire(ctx, w.size); err != nil {
		return fmt.Errorf("failed to wait for all jobs to finish: %w", err)
	}
	defer w.sem.Release(w.size)

	return nil
}

// Done immediately stops the worker and prevents new work from being enqueued.
// Then it waits for all existing work to finish and results the results.
//
// The results are returned in the order in which jobs were enqueued into the
// worker. Each result will include a result value or corresponding error type.
// The function itself returns an error only if the context is cancelled.
//
// If the worker is already done, it returns [ErrStopped].
func (w *Worker[T]) Done(ctx context.Context) ([]*Result[T], error) {
	if !atomic.CompareAndSwapUint32(&w.stopped, 0, 1) {
		return nil, ErrStopped
	}

	if err := w.sem.Acquire(ctx, w.size); err != nil {
		return nil, err
	}
	defer w.sem.Release(w.size)

	w.resultsLock.Lock()
	defer w.resultsLock.Unlock()

	// Fix insertion order.
	final := make([]*Result[T], len(w.results))
	for _, v := range w.results {
		final[v.idx] = v.result
	}
	return final, nil
}

// isStopped returns true if the worker is stopped, false otherwise. It is safe
// for concurrent use.
func (w *Worker[T]) isStopped() bool {
	return atomic.LoadUint32(&w.stopped) == 1
}
