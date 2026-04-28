package writ

import (
	"fmt"
	"net/http"
)

// handleResolverError walks the route's compiled error-entry slice
// and dispatches the resolver error per the spec's *Type Matching*
// rules:
//
//  1. Walk the slice; for each non-default entry, invoke its matcher.
//     The first match wins — invoke its registered formatter.
//  2. If no concrete-type entry matched, fall back to the first
//     `default` entry encountered during the walk.
//  3. If neither path matched, fall through to the *Status
//     Resolution* matrix's "No errors-block match" rows: write the
//     plain-text status response when the error implements
//     StatusCode, or the spec-003 generic 500 otherwise.
func handleResolverError(rw http.ResponseWriter, req *http.Request, route *compiledRoute, err error) {
	status, hasStatusCode := resolveStatus(err)

	var defaultEntry *compiledErrorEntry
	for i := range route.errorEntries {
		e := &route.errorEntries[i]
		if e.isDefault {
			if defaultEntry == nil {
				defaultEntry = e
			}
			continue
		}
		if e.matcher != nil && e.matcher(err) {
			invokeErrorFormatter(rw, req, e, err, status)
			return
		}
	}

	if defaultEntry != nil {
		invokeErrorFormatter(rw, req, defaultEntry, err, status)
		return
	}

	if hasStatusCode {
		writeStatusText(rw, status)
		return
	}
	write500(rw)
}

// invokeErrorFormatter wraps rw with a writeRecorder and runs the
// formatter under the same pre-/post-write rules as the success-path
// formatter: a non-nil formatter return that occurred before any
// bytes were written produces the runtime-owned 500; an error after
// writing leaves the partial response in flight.
func invokeErrorFormatter(rw http.ResponseWriter, req *http.Request, entry *compiledErrorEntry, err error, status int) {
	recorder := &writeRecorder{ResponseWriter: rw}
	data := ErrorData{err: err, status: status, request: req}
	if formatErr := entry.formatter(req.Context(), recorder, data); formatErr != nil {
		if !recorder.hasWritten {
			write500(rw)
		}
	}
}

// resolveStatus inspects err for the documented `StatusCode() int`
// method shape and returns the resolved status plus a flag that
// distinguishes "explicitly opted in to a status" (true) from
// "defaulted to 500" (false). The boolean drives the Q3 fallback
// matrix: the no-match-but-has-StatusCode path writes a plain-text
// status line; the no-match-no-StatusCode path keeps the spec-003
// generic 500 body.
//
// The check uses an anonymous interface assertion so error types do
// not need to import writ to opt in. The runtime tests the immediate
// concrete type only; wrappers that want the inner status to surface
// implement StatusCode themselves.
func resolveStatus(err error) (status int, hasStatusCode bool) {
	if sc, ok := err.(interface{ StatusCode() int }); ok {
		return sc.StatusCode(), true
	}
	return http.StatusInternalServerError, false
}

// writeStatusText writes the plain-text status response used by the
// Q3 "no errors-block match, but StatusCode() present" path.
// Symmetric with [write500]: sets Content-Type, writes the status,
// then writes "<status> <reason>\n" using http.StatusText. Falls
// back to "Error" when the standard library has no reason phrase
// for the given code.
func writeStatusText(rw http.ResponseWriter, status int) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(status)
	text := http.StatusText(status)
	if text == "" {
		text = "Error"
	}
	_, _ = fmt.Fprintf(rw, "%d %s\n", status, text)
}
