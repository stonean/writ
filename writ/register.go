package writ

import (
	"context"
	"fmt"
	"net/http"
)

// ResolverFunc is the Go signature for `resolve` step
// implementations. The runtime invokes a registered resolver once
// per matching request, passing a Params restricted to the route
// parameters listed in the DSL call site. The returned value is
// stored under the resolve step's variable name and is visible to
// the format step (and to subsequent resolve steps once future
// features wire field references).
//
// A non-nil error from a resolver short-circuits the request with a
// generic 500 response.
type ResolverFunc func(ctx context.Context, params Params) (any, error)

// FormatterFunc is the Go signature for `format` step
// implementations. The runtime invokes the registered formatter once
// per matching request, passing a Results restricted to the names
// listed in the format line's `with` clause. The formatter is
// responsible for writing the response status, headers, and body.
//
// Returning a non-nil error before writing produces a generic 500
// response. Returning an error after writing leaves the partial
// response in flight (no rewind possible per net/http semantics).
type FormatterFunc func(ctx context.Context, w http.ResponseWriter, data Results) error

// Resolver registers fn under name. Returns an error when name is
// already registered. Panics when called after [Writ.Load] (state is
// no longer stateInit).
//
// Tests that need different resolver implementations across cases
// build a fresh [New] instance per case rather than mutating an
// existing instance's registrations.
func (w *Writ) Resolver(name string, fn ResolverFunc) error {
	w.mustBeInit("Resolver")
	if _, exists := w.resolvers[name]; exists {
		return fmt.Errorf("resolver %q is already registered", name)
	}
	w.resolvers[name] = fn
	return nil
}

// Formatter registers fn under name. Returns an error when name is
// already registered. Panics when called after [Writ.Load] (state is
// no longer stateInit).
func (w *Writ) Formatter(name string, fn FormatterFunc) error {
	w.mustBeInit("Formatter")
	if _, exists := w.formatters[name]; exists {
		return fmt.Errorf("formatter %q is already registered", name)
	}
	w.formatters[name] = fn
	return nil
}

// mustBeInit panics with a clear message when the runtime has moved
// beyond stateInit. The caller name appears in the panic message so
// the developer can locate the offending registration.
func (w *Writ) mustBeInit(caller string) {
	switch w.state.Load() {
	case stateInit:
		return
	case stateLoading:
		panic(fmt.Sprintf("writ: %s called while Load is in progress", caller))
	case stateLoaded:
		panic(fmt.Sprintf("writ: %s called after Load; registrations must complete before loading", caller))
	}
}
