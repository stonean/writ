package writ

import (
	"fmt"

	"github.com/stonean/writ/parser"
	"github.com/stonean/writ/pipeline"
)

// Load reads and compiles the .writ program at path. It runs five
// strictly-ordered passes:
//
//  1. parser.Parse        → KindParseFailure entries on lex/parse errors
//  2. pipeline.Elaborate  → KindElaborationFailure entries on elaborator errors
//  3. validate            → per-stage validation + pipeline-shape backstop
//  4. checkRouteAmbiguity → method+canonical-path collisions (folded into validate)
//  5. compileRoutes       → produces the immutable routingTable (folded into validate)
//
// Parse and elaboration are short-circuit gates: if either pass
// produces entries, subsequent passes do not run because the AST or
// resolved structure is too partial to reason about safely.
//
// On any non-empty entries slice Load returns a non-nil *Error
// whose Entries are in source order, and the runtime is rolled
// back to its initial state. On success the routing table is
// installed via atomic.Pointer and the runtime transitions to
// stateLoaded.
//
// Concurrent invocation of Load on the same instance is a
// programming error and produces a runtime panic. After a
// successful load, a second Load returns [ErrAlreadyLoaded].
func (w *Writ) Load(path string) error {
	if !w.state.CompareAndSwap(stateInit, stateLoading) {
		switch w.state.Load() {
		case stateLoading:
			panic("writ: Load called while another Load is already in progress on this instance")
		default: // stateLoaded
			return ErrAlreadyLoaded
		}
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			return
		}
		w.state.Store(stateInit)
	}()

	// Pass 1: parse.
	prog, parseErrs := parser.Parse(path)
	if len(parseErrs) > 0 {
		rolledBack = true
		return aggregateParseErrors(parseErrs)
	}

	// Pass 2: pipeline elaboration.
	resolved, elabErrs := pipeline.Elaborate(prog)
	if len(elabErrs) > 0 {
		rolledBack = true
		return aggregateElaborationErrors(elabErrs)
	}

	// Pass 3+4+5: validation orchestrates per-stage checks, the
	// pipeline-shape backstop, and route ambiguity.
	table, entries := validate(resolved, w.resolvers, w.formatters, w.errorFormatters, w.errorTypes)
	if len(entries) > 0 {
		rolledBack = true
		return &Error{Entries: entries}
	}

	// Success: install the routing table and transition forward.
	w.table.Store(table)
	w.state.Store(stateLoaded)
	return nil
}

// aggregateParseErrors converts a parser error slice into an
// *Error with one KindParseFailure entry per parser.Error,
// preserving span and message.
func aggregateParseErrors(errs []parser.Error) *Error {
	entries := make([]Entry, 0, len(errs))
	for _, e := range errs {
		entries = append(entries, Entry{
			Kind:    KindParseFailure,
			Message: fmt.Sprintf("parser: %s", e.Message),
			Span:    e.Span,
		})
	}
	return &Error{Entries: entries}
}

// aggregateElaborationErrors converts a pipeline error slice into
// an *Error with one KindElaborationFailure entry per
// pipeline.Error, preserving span(s) and message. The elaborator's
// own typed Kind is collapsed into KindElaborationFailure;
// consumers that need the underlying classification should run
// pipeline.Elaborate directly.
func aggregateElaborationErrors(errs []pipeline.Error) *Error {
	entries := make([]Entry, 0, len(errs))
	for _, e := range errs {
		entries = append(entries, Entry{
			Kind:    KindElaborationFailure,
			Message: fmt.Sprintf("elaboration: %s", e.Message),
			Span:    e.Span,
			Spans:   e.Spans,
		})
	}
	return &Error{Entries: entries}
}
