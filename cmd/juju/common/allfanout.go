// Copyright 2026 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"fmt"
	"sort"
	"sync"

	"github.com/juju/errors"

	"github.com/juju/juju/api/jujuclient"
	"github.com/juju/juju/cmd/cmd"
)

// ControllerResult holds the outcome of a per-controller operation performed
// by FanOutToControllers.
type ControllerResult[T any] struct {
	// ControllerName is the name of the controller this result came from.
	ControllerName string
	// Value is the result produced by the operation, valid only when Err is nil.
	Value T
	// Err is non-nil when the operation failed for this controller.
	Err error
}

// FanOutToControllerNames queries the supplied controllers concurrently,
// calling op for each one. Results are returned in the same order as names.
//
// This is the primitive fan-out used by both the single-controller and
// all-controllers paths; callers that always want every registered controller
// should use FanOutToControllers instead.
func FanOutToControllerNames[T any](names []string, op func(controllerName string) (T, error)) []ControllerResult[T] {
	results := make([]ControllerResult[T], len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, controllerName string) {
			defer wg.Done()
			val, err := op(controllerName)
			results[i] = ControllerResult[T]{
				ControllerName: controllerName,
				Value:          val,
				Err:            err,
			}
		}(i, name)
	}
	wg.Wait()
	return results
}

// FanOutToControllers queries every controller in the client store
// concurrently, calling op for each one. Results are returned in
// deterministic (sorted-by-controller-name) order.
//
// This is the right tool for read-only operations that aggregate results
// across all controllers (e.g. listing models, searching for offers). It is
// NOT appropriate for operations that must search sequentially and stop at the
// first hit (e.g. offer consumption) — implement those directly.
func FanOutToControllers[T any](store jujuclient.ClientStore, op func(controllerName string) (T, error)) ([]ControllerResult[T], error) {
	all, err := store.AllControllers()
	if err != nil {
		return nil, errors.Trace(err)
	}
	if len(all) == 0 {
		return nil, errors.New("no controllers registered")
	}

	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return FanOutToControllerNames(names, op), nil
}

// WarnOnControllerErrors writes a warning line to stderr for every result
// whose Err is non-nil, and returns whether any successful results remain.
// It is a convenience helper for commands using FanOutToControllers that want
// the standard "could not <verb> controller <name>: <err>" warning format.
func WarnOnControllerErrors[T any](ctx *cmd.Context, verb string, results []ControllerResult[T]) (anySucceeded bool) {
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(ctx.GetStderr(), "could not %s controller %q: %v\n", verb, r.ControllerName, r.Err)
			continue
		}
		anySucceeded = true
	}
	return anySucceeded
}

// CollectResults separates a slice of ControllerResults into successful values
// and per-controller errors. When only one result is present and it errored,
// the error is returned directly rather than as a per-controller warning, so
// single-controller callers get a plain error and multi-controller callers get
// warnings.
//
// Typical usage:
//
//	values, err := common.CollectResults(ctx, "search", results)
//	if err != nil { return err }
func CollectResults[T any](ctx *cmd.Context, verb string, results []ControllerResult[T]) (values []T, err error) {
	for _, r := range results {
		if r.Err == nil {
			values = append(values, r.Value)
			continue
		}
		if len(results) == 1 {
			// Single target: surface the error directly.
			return nil, errors.Trace(r.Err)
		}
		fmt.Fprintf(ctx.GetStderr(), "could not %s controller %q: %v\n", verb, r.ControllerName, r.Err)
	}
	return values, nil
}

// CandidateControllers returns an ordered list of controller names to search
// when the target controller is not named explicitly. The current controller
// is always first; remaining registered controllers follow in sorted order.
//
// This is the right building block for sequential first-match searches (e.g.
// "find the controller that hosts offer X", "find a controller that can
// accept a new model"). The caller is responsible for the iteration and the
// domain-specific stop condition; only the list construction is shared here.
//
// Pass allControllers=false (or when a single-controller test stub is active)
// to restrict the candidates to the current controller only.
func CandidateControllers(store jujuclient.ClientStore, current string, allControllers bool) ([]string, error) {
	if !allControllers {
		return []string{current}, nil
	}

	all, err := store.AllControllers()
	if err != nil {
		return nil, errors.Trace(err)
	}

	rest := make([]string, 0, len(all))
	for name := range all {
		if name != current {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append([]string{current}, rest...), nil
}
