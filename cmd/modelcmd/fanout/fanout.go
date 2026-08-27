// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package fanout provides a reusable, thread-safe helper for running a
// read-only query concurrently across every controller registered in a
// jujuclient.ClientStore.
//
// The helper resolves account credentials per controller (a user may hold
// different identities on different controllers) and opens an isolated
// api.Connection per worker through an injected Opener, so callers do not
// race on shared CommandBase connection caches.
//
// Failure handling is fail-soft: a controller that fails to connect or query
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

// Opener returns an API connection for the named controller.
//
// The connection is owned by the helper: Run closes every connection it
// opened, including ones for which the worker returned an error. Opener
// MUST NOT touch any unsynchronized connection cache shared across
// goroutines; it should open a fresh connection per call.
type Opener func(ctx context.Context, controllerName string) (api.Connection, error)

// WorkerFunc is the per-controller worker. The helper resolves the
// controller's AccountDetails and opens a connection before invoking worker,
// so worker receives the connection and account that belong to this
// controller only.
type WorkerFunc[T any] func(
	ctx context.Context,
	conn api.Connection,
	controllerName string,
	account jujuclient.AccountDetails,
) (T, error)

// Result holds the outcome of a single controller's worker invocation.
// ControllerName is always populated. Err is non-nil if the opener or the
// worker failed for this controller.
type Result[T any] struct {
	// ControllerName is the name of the controller this result is for.
	ControllerName string

	// Data is the value returned by the worker for this controller. It is
	// the zero value of T when Err is non-nil.
	Data T

	// Err is the error from opening the connection or running the worker.
	Err error
}

// Run fans worker out concurrently across the named controllers. The caller
// decides which controllers to visit; RunAll visits every controller
// registered in store. Controllers are visited in the order given, but the
// returned slice is in the same order as controllerNames so callers can
// render output deterministically regardless of which worker finished first.
//
// For each controller the helper:
//  1. resolves AccountDetails(controllerName); a missing account is a
//     per-controller error (the controller is skipped, not a fatal abort),
//  2. opens an api.Connection via opener,
//  3. invokes worker with the connection, controller name, and account,
//  4. closes the connection.
//
// The context passed to opener and worker is the same ctx passed to Run; a
// cancelled context will cause in-flight workers to observe cancellation
// according to the opener and worker's context handling.
func Run[T any](
	ctx context.Context,
	store jujuclient.ClientStore,
	opener Opener,
	worker WorkerFunc[T],
	controllerNames []string,
) []Result[T] {
	results := make([]Result[T], len(controllerNames))
	if len(controllerNames) == 0 {
		return results
	}
	var wg sync.WaitGroup
	for i, name := range controllerNames {
		wg.Add(1)
		go func(i int, controllerName string) {
			defer wg.Done()
			results[i] = runOne(ctx, store, opener, worker, controllerName)
		}(i, name)
	}
	wg.Wait()
	return results
}

// RunAll fans worker out concurrently across every controller registered in
// store, in sorted, deterministic order. It is a convenience wrapper around
// Run for the common case. If AllControllers fails, the error is surfaced as
// a single Result with an empty controller name.
func RunAll[T any](
	ctx context.Context,
	store jujuclient.ClientStore,
	opener Opener,
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
	return Run(ctx, store, opener, worker, names)
}

// runOne is the per-controller worker. It is safe to call concurrently
// because it reads only per-controller store entries and uses the injected
// opener, which the caller guarantees does not touch shared unsynchronized
// state.
func runOne[T any](
	ctx context.Context,
	store jujuclient.ClientStore,
	opener Opener,
	worker WorkerFunc[T],
	controllerName string,
) Result[T] {
	var zero T
	res := Result[T]{ControllerName: controllerName, Data: zero}

	account, err := store.AccountDetails(controllerName)
	if err != nil {
		res.Err = err
		return res
	}

	conn, err := opener(ctx, controllerName)
	if err != nil {
		res.Err = err
		return res
	}
	if conn != nil {
		defer func() { _ = conn.Close() }()
	}

	data, err := worker(ctx, conn, controllerName, *account)
	res.Data = data
	res.Err = err
	return res
}
