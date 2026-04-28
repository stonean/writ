package writ

import (
	"context"
	"net/http"
)

// ErrorFormatterFunc is the Go signature for `errors` block formatter
// implementations. The runtime invokes a registered error formatter
// when a resolver returns a non-nil error and the handler's effective
// error map matches. The formatter writes the response status,
// headers, and body.
//
// Symmetric in shape with [FormatterFunc]; the third argument is the
// [ErrorData] accessor instead of [Results].
type ErrorFormatterFunc func(ctx context.Context, w http.ResponseWriter, data ErrorData) error

// ErrorData is the error-formatter-side accessor for the failing
// error, the resolved status, and the originating request. The
// runtime constructs a fresh ErrorData per error-handling pass and
// passes it to the formatter by value.
//
// Future iterations may add accessors (parsed body, partial resolver
// results, request-id, retry hints) without changing the type's
// signature.
type ErrorData struct {
	err     error
	status  int
	request *http.Request
}

// Err returns the value the resolver returned. It may be a typed
// struct, a pointer to a struct, or a wrapped error; errors.As and
// errors.Is work on it.
func (d ErrorData) Err() error { return d.err }

// Status returns the status the runtime resolved before invoking the
// formatter: the value of StatusCode() when the error implements it,
// otherwise 500.
func (d ErrorData) Status() int { return d.status }

// Request returns the live *http.Request the dispatcher received from
// net/http. Formatters use it for path-aware logging, request-id
// headers, content-type sniffing, and similar concerns.
func (d ErrorData) Request() *http.Request { return d.request }
