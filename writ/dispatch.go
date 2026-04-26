package writ

import (
	"net/http"
	"strings"
)

// Handler returns the runtime as a [net/http] handler. It panics
// if [Writ.Load] has not been called successfully on this instance.
//
// The returned handler is safe to compose with arbitrary
// middleware: it satisfies http.Handler with no runtime-specific
// adapters.
func (w *Writ) Handler() http.Handler {
	if w.state.Load() != stateLoaded {
		panic("writ: Handler called before Load completed; call Load first")
	}
	return w
}

// ServeHTTP dispatches an incoming request through the runtime
// pipeline: route match → resolve steps → format step. Failures at
// any stage produce the appropriate HTTP response (404, 405, or a
// runtime-owned 500) without invoking the formatter.
func (w *Writ) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if w.state.Load() != stateLoaded {
		// Defensive: Handler panics before this is reachable, but
		// guard so a misuse via direct ServeHTTP call still
		// produces a response instead of a server-side panic that
		// closes the connection.
		write500(rw)
		return
	}

	table := w.table.Load()
	route, params, allow := table.match(req.Method, req.URL.Path)

	if route == nil {
		if len(allow) > 0 {
			rw.Header().Set("Allow", strings.Join(allow, ", "))
			http.Error(rw, "405 method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(rw, req)
		return
	}

	ctx := req.Context()
	results := make(map[string]any, len(route.resolves))
	for _, step := range route.resolves {
		val, err := step.fn(ctx, paramsForCall(params, step.paramArgs))
		if err != nil {
			write500(rw)
			return
		}
		results[step.name] = val
	}

	formatterResults := buildResults(results, route.format.with)
	recorder := &writeRecorder{ResponseWriter: rw}
	if err := route.format.fn(ctx, recorder, formatterResults); err != nil {
		if !recorder.hasWritten {
			write500(rw)
		}
		// If the formatter has already started writing the response
		// the runtime accepts the partial response and moves on. A
		// future logging feature will record the error.
		return
	}
}

// paramsForCall returns a Params containing only the route-parameter
// values listed in paramArgs. The dispatcher uses this to enforce
// the spec contract that a resolver observes only the route
// parameters its DSL call site listed as `:name` arguments.
func paramsForCall(all Params, paramArgs []string) Params {
	if len(paramArgs) == 0 {
		return Params{}
	}
	values := make(map[string]string, len(paramArgs))
	for _, name := range paramArgs {
		if v, ok := all.values[name]; ok {
			values[name] = v
		}
	}
	return Params{values: values}
}

// buildResults projects the per-request results map onto the names
// listed in the format line's `with` clause. Names not in `with`
// are not visible to the formatter.
func buildResults(all map[string]any, with []string) Results {
	if len(with) == 0 {
		return Results{}
	}
	values := make(map[string]any, len(with))
	for _, name := range with {
		if v, ok := all[name]; ok {
			values[name] = v
		}
	}
	return Results{values: values}
}

// write500 writes the runtime-owned 500 response. The body is
// intentionally generic; resolver and formatter error details are
// not exposed to the client. Future logging features will surface
// the underlying error to operators.
func write500(rw http.ResponseWriter) {
	rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rw.WriteHeader(http.StatusInternalServerError)
	_, _ = rw.Write([]byte("500 Internal Server Error\n"))
}

// writeRecorder wraps an http.ResponseWriter and tracks whether
// any bytes have been emitted yet. The dispatcher uses hasWritten
// to decide whether a formatter error can still be turned into a
// runtime-owned 500: once the formatter has started writing the
// response, the runtime cannot rewind it.
type writeRecorder struct {
	http.ResponseWriter
	hasWritten bool
}

func (r *writeRecorder) WriteHeader(status int) {
	r.hasWritten = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *writeRecorder) Write(p []byte) (int, error) {
	r.hasWritten = true
	return r.ResponseWriter.Write(p)
}
