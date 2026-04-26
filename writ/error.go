package writ

import (
	"errors"
	"fmt"
	"strings"

	"github.com/stonean/writ/ast"
)

// ErrorKind classifies a startup-time problem reported by [Writ.Load].
// Consumers can switch on Kind without parsing message strings.
type ErrorKind int

const (
	// KindParseFailure carries a parser error from spec 001.
	KindParseFailure ErrorKind = iota
	// KindElaborationFailure carries an elaborator error from spec 002.
	KindElaborationFailure
	// KindUnregisteredResolver indicates a `resolve` step in the DSL
	// references a resolver name that was not registered via
	// [Writ.Resolver].
	KindUnregisteredResolver
	// KindUnregisteredFormatter indicates a `format` step in the DSL
	// references a formatter name that was not registered via
	// [Writ.Formatter].
	KindUnregisteredFormatter
	// KindUnsupportedStage indicates a handler's effective pipeline
	// includes a stage outside the in-scope envelope. Spec 003 supports
	// only `resolve` and `format`; every other stage produces this kind.
	KindUnsupportedStage
	// KindUndeclaredRouteParameter indicates a resolver argument
	// `:name` does not resolve to a parameter declared in the
	// handler's route pattern.
	KindUndeclaredRouteParameter
	// KindRouteAmbiguity indicates two or more handlers are declared
	// on the same method-and-path.
	KindRouteAmbiguity
	// KindMissingEnvVar indicates a required environment variable
	// did not resolve. Reserved for symmetry with the Configuration
	// future feature; the skeleton iteration emits no entries of
	// this kind because PORT and WRIT_ENV both have defaults.
	KindMissingEnvVar
)

// String returns the lowercase identifier of the kind, suitable for
// inclusion in human-readable error messages.
func (k ErrorKind) String() string {
	switch k {
	case KindParseFailure:
		return "parse-failure"
	case KindElaborationFailure:
		return "elaboration-failure"
	case KindUnregisteredResolver:
		return "unregistered-resolver"
	case KindUnregisteredFormatter:
		return "unregistered-formatter"
	case KindUnsupportedStage:
		return "unsupported-stage"
	case KindUndeclaredRouteParameter:
		return "undeclared-route-parameter"
	case KindRouteAmbiguity:
		return "route-ambiguity"
	case KindMissingEnvVar:
		return "missing-env-var"
	}
	return fmt.Sprintf("ErrorKind(%d)", int(k))
}

// Entry is one structured problem in an [Error] aggregate. Spans
// reference the originating AST node when available; for runtime
// errors that cannot be tied to a single span (e.g. missing env vars)
// the zero Span is acceptable.
type Entry struct {
	Kind    ErrorKind
	Message string
	Span    ast.Span
	Spans   []ast.Span // additional spans (route ambiguity lists every conflicting handler); nil otherwise
}

// Error formats the entry as `file:line:col: kind: message`. The
// file/line/column come from the entry's primary Span; entries
// without a meaningful span format with empty location fields.
func (e Entry) Error() string {
	if e.Span.Start.Source == nil {
		return fmt.Sprintf("%s: %s", e.Kind, e.Message)
	}
	return fmt.Sprintf("%s:%d:%d: %s: %s",
		e.Span.Start.Source.Path,
		e.Span.Start.Line,
		e.Span.Start.Column,
		e.Kind,
		e.Message,
	)
}

// Error is the aggregate startup error returned by [Writ.Load]. It
// wraps every problem found in a single load attempt; callers can
// access the typed list via Entries or pattern-match via errors.As.
type Error struct {
	Entries []Entry
}

// Error formats every entry on its own line, joined with newlines.
// An empty aggregate produces an empty string; callers should check
// len(Entries) before relying on the message for display.
func (e *Error) Error() string {
	if e == nil || len(e.Entries) == 0 {
		return ""
	}
	parts := make([]string, len(e.Entries))
	for i, entry := range e.Entries {
		parts[i] = entry.Error()
	}
	return strings.Join(parts, "\n")
}

// Unwrap returns each Entry as an individual error so errors.As can
// match on entry-level state. Callers needing the typed entry list
// should read Entries directly. Returns nil when the aggregate is
// empty.
func (e *Error) Unwrap() []error {
	if e == nil || len(e.Entries) == 0 {
		return nil
	}
	out := make([]error, len(e.Entries))
	for i, entry := range e.Entries {
		out[i] = entry
	}
	return out
}

// Sentinel for assertions like errors.Is(err, ErrAlreadyLoaded). Not
// a startup-validation kind; produced by [Writ.Load] when invoked on
// an already-loaded instance.
var ErrAlreadyLoaded = errors.New("writ: runtime is already loaded")
