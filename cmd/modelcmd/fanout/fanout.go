// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package fanout provides a reusable helper for running a read-only query
// concurrently across every controller registered in a
// jujuclient.ClientStore.
//
// The helper resolves account credentials per controller (a user may hold
// different identities on different controllers) and hands each worker an
// isolated Session, so callers do not race on shared CommandBase connection
// caches.
//
// Setup runs sequentially on the calling goroutine before any worker starts.
// This keeps connection establishment - which may prompt for credentials
// or touch unsynchronized command state such as CommandBase's API context
// cache - free of data races and interleaved prompts. Only the query phase
// fans out concurrently.
//
// Failure handling is fail-soft: a controller that fails to set up or query
// is recorded as an error on its Result and does not abort the rest of the
// fan-out. Callers are responsible for downgrading per-controller errors to
// stderr warnings and for deciding the exit code based on whether any
// controller returned data.
package fanout

import (
	"context"
	"sort"
	"sync"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/jujuclient"
)

// Session holds the per-controller connection and account resolved during
// the sequential setup phase and handed to the worker.
type Session struct {
	// Conn is the API connection for the controller. It may be nil when
	// the SetupFunc substitutes a non-connection client (e.g. in tests);
	// Run only closes non-nil connections.
	Conn api.Connection

	// Account is the resolved account for the controller.
	Account jujuclient.AccountDetails
}

// SetupFunc prepares a Session for the named controller. It is called
// sequentially on the calling goroutine, once per controller, before any
// worker runs, so it may safely prompt for credentials or touch
// unsynchronized command state. A returned error fails only that
// controller.
type SetupFunc func(ctx context.Context, controllerName string) (Session, error)

// WorkerFunc is the per-controller worker. It receives the Session built
// during setup and must be safe for concurrent use: it runs on its own
// goroutine for each controller. The worker does not own the connection;
// Run closes it after the worker returns.
type WorkerFunc[T any] func(
	ctx context.Context,
	session Session,
	controllerName string,
) (T, error)

// Result holds the outcome of a single controller's worker invocation.
// ControllerName is always populated. Err is non-nil if setup or the
// worker failed for this controller.
type Result[T any] struct {
	// ControllerName is the name of the controller this result is for.
	ControllerName string

	// Data is the value returned by the worker for this controller. It is
	// the zero value of T when Err is non-nil.
	Data T

	// Err is the error from setting up or running the worker.
	Err error
}

// Run fans worker out concurrently across the named controllers. The
// returned slice is in the same order as controllerNames so callers can
// render output deterministically regardless of which worker finished
// first.
//
// For each controller, in order and on the calling goroutine, Run first
// invokes setup. Controllers whose setup fails are recorded as an error
// Result and skipped; setup for the remaining controllers still runs. Once
// all setups are done, the workers for the successfully set-up controllers
// run concurrently, each on its own goroutine, and each connection is
// closed when its worker returns.
//
// The context passed to setup and worker is the same ctx passed to Run; a
// cancelled context will cause in-flight workers to observe cancellation
// according to the setup and worker's context handling.
func Run[T any](
	ctx context.Context,
	setup SetupFunc,
	worker WorkerFunc[T],
	controllerNames []string,
) []Result[T] {
	results := make([]Result[T], len(controllerNames))
	for i, name := range controllerNames {
		results[i].ControllerName = name
	}
	sessions := make([]Session, len(controllerNames))
	for i, name := range controllerNames {
		session, err := setup(ctx, name)
		if err != nil {
			var zero T
			results[i].Data = zero
			results[i].Err = err
			continue
		}
		sessions[i] = session
	}

	var wg sync.WaitGroup
	for i := range controllerNames {
		if results[i].Err != nil {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data, err := worker(ctx, sessions[i], controllerNames[i])
			results[i].Data = data
			results[i].Err = err
		}(i)
	}
	wg.Wait()

	// Close every connection that was opened, including those whose
	// worker failed.
	for i := range results {
		if sessions[i].Conn != nil {
			_ = sessions[i].Conn.Close()
		}
	}
	return results
}

// RunAll fans worker out concurrently across every controller registered
// in store, in sorted, deterministic order. If enumerating the
// controllers fails, the error is surfaced as a single Result with an
// empty controller name.
func RunAll[T any](
	ctx context.Context,
	store jujuclient.ClientStore,
	setup SetupFunc,
	worker WorkerFunc[T],
) []Result[T] {
	controllers, err := store.AllControllers()
	if err != nil {
		var zero T
		return []Result[T]{{ControllerName: "", Data: zero, Err: err}}
	}
	names := make([]string, 0, len(controllers))
	for name := range controllers {
		names = append(names, name)
	}
	sort.Strings(names)
	return Run(ctx, setup, worker, names)
}

// AllFailed reports whether every result carries an error. Callers can
// use it to decide the exit code: a fan-out where no controller returned
// data should not exit zero.
func AllFailed[T any](results []Result[T]) bool {
	for _, r := range results {
		if r.Err == nil {
			return false
		}
	}
	return len(results) > 0
}
