package writ

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stonean/writ/ast"
	"github.com/stonean/writ/pipeline"
)

// routingTable holds every compiled handler grouped by HTTP method
// plus a pre-sorted list of declared methods used for fast Allow
// header construction. The table is built once during Load and is
// immutable thereafter.
type routingTable struct {
	byMethod map[string][]*compiledRoute
	methods  []string
}

// compiledRoute is a single handler ready for request-time
// dispatch. Resolver and formatter pointers are looked up at
// compile time so the request path performs no map lookups against
// the registration tables.
type compiledRoute struct {
	method     string
	segments   []ast.RouteSegment
	paramNames []string
	resolves   []resolveStep
	format     formatStep
	span       ast.Span
}

// resolveStep carries everything the dispatcher needs to invoke a
// resolver: the variable name to store the result under, the
// pre-resolved Go function, and the route-parameter names listed in
// the DSL call site. paramArgs preserves source order.
type resolveStep struct {
	name      string
	fn        ResolverFunc
	paramArgs []string
}

// formatStep carries the pre-resolved formatter and the with-clause
// names. with preserves source order.
type formatStep struct {
	template string
	fn       FormatterFunc
	with     []string
}

// compileRoutes walks every handler in resolved and produces a
// routingTable. Validation errors are returned via the entries
// slice; on a non-empty entries slice the routingTable is partial
// (handlers that compiled cleanly are present) and callers should
// treat the load as failed.
func compileRoutes(
	resolved *pipeline.Resolved,
	resolvers map[string]ResolverFunc,
	formatters map[string]FormatterFunc,
) (*routingTable, []Entry) {
	table := &routingTable{byMethod: make(map[string][]*compiledRoute)}
	var entries []Entry
	methodsSeen := map[string]struct{}{}

	for _, h := range resolved.Handlers {
		route, hEntries := compileHandler(h, resolvers, formatters)
		entries = append(entries, hEntries...)
		if route == nil {
			continue
		}
		table.byMethod[route.method] = append(table.byMethod[route.method], route)
		if _, ok := methodsSeen[route.method]; !ok {
			methodsSeen[route.method] = struct{}{}
			table.methods = append(table.methods, route.method)
		}
	}

	sort.Strings(table.methods)
	return table, entries
}

// compileHandler builds one *compiledRoute or returns nil with
// entries describing why the handler could not be compiled.
func compileHandler(
	h *pipeline.Handler,
	resolvers map[string]ResolverFunc,
	formatters map[string]FormatterFunc,
) (*compiledRoute, []Entry) {
	var entries []Entry

	paramNames := routeParamNames(h.Pattern)
	route := &compiledRoute{
		method:     h.Method,
		segments:   h.Pattern.Segments,
		paramNames: paramNames,
		span:       h.Span,
	}

	var formatSet bool
	for _, stage := range h.Stages {
		switch s := stage.(type) {
		case *pipeline.ResolveStage:
			step, stepEntries := compileResolve(s, paramNames, resolvers)
			entries = append(entries, stepEntries...)
			if step != nil {
				route.resolves = append(route.resolves, *step)
			}
		case *pipeline.FormatStage:
			step, stepEntries := compileFormat(s, formatters)
			entries = append(entries, stepEntries...)
			if step != nil {
				route.format = *step
				formatSet = true
			}
		default:
			entries = append(entries, Entry{
				Kind:    KindUnsupportedStage,
				Message: fmt.Sprintf("stage %s is not supported in the runtime skeleton; see specs/003-runtime-skeleton", stage.Kind()),
				Span:    stage.Span(),
			})
		}
	}

	// A handler that did not compile a format step cannot be served.
	// The pipeline elaborator will normally have already produced an
	// error in this case (no valid handler ends without format), but
	// guard defensively so the routing table never contains a route
	// without a formatter.
	if !formatSet {
		return nil, entries
	}
	if len(entries) > 0 {
		return nil, entries
	}
	return route, nil
}

// compileResolve resolves one ResolveStage into a resolveStep,
// emitting entries for any unregistered resolver name or invalid
// argument shape.
func compileResolve(
	s *pipeline.ResolveStage,
	paramNames []string,
	resolvers map[string]ResolverFunc,
) (*resolveStep, []Entry) {
	var entries []Entry
	call := s.Call()
	fn, ok := resolvers[call.Name]
	if !ok {
		entries = append(entries, Entry{
			Kind:    KindUnregisteredResolver,
			Message: fmt.Sprintf("resolver %q is not registered", call.Name),
			Span:    s.Span(),
		})
	}

	paramArgs := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		switch a := arg.(type) {
		case *ast.RouteParamRef:
			if !contains(paramNames, a.Name) {
				entries = append(entries, Entry{
					Kind:    KindUndeclaredRouteParameter,
					Message: fmt.Sprintf("parameter %q is not declared in the handler's route", ":"+a.Name),
					Span:    a.Span(),
				})
				continue
			}
			paramArgs = append(paramArgs, a.Name)
		default:
			// FieldRef, BodyRef, QueryRef, NamedArg, literals: out
			// of scope per spec 003 *In Scope*. Reported under
			// KindUndeclaredRouteParameter (no kind exists for
			// "unsupported argument shape" because the skeleton's
			// in-scope subset has only one shape — a route
			// parameter reference).
			entries = append(entries, Entry{
				Kind:    KindUndeclaredRouteParameter,
				Message: fmt.Sprintf("argument is not a `:name` route parameter reference; only :name references are supported in the runtime skeleton (got %T)", arg),
				Span:    arg.Span(),
			})
		}
	}

	if len(entries) > 0 {
		return nil, entries
	}
	return &resolveStep{name: s.Name(), fn: fn, paramArgs: paramArgs}, nil
}

// compileFormat resolves one FormatStage into a formatStep, emitting
// entries for unregistered formatter names. The with-list is taken
// verbatim from the AST.
func compileFormat(
	s *pipeline.FormatStage,
	formatters map[string]FormatterFunc,
) (*formatStep, []Entry) {
	var entries []Entry
	fn, ok := formatters[s.Template()]
	if !ok {
		entries = append(entries, Entry{
			Kind:    KindUnregisteredFormatter,
			Message: fmt.Sprintf("formatter %q is not registered", s.Template()),
			Span:    s.Span(),
		})
	}

	with := make([]string, 0, len(s.Data()))
	for _, ref := range s.Data() {
		// In-scope `with` clause names are bare resolver-result
		// names (no dotted Path). Path is reserved for field-
		// reference features; reject here for symmetry with
		// resolver argument validation.
		if len(ref.Path) > 0 {
			entries = append(entries, Entry{
				Kind:    KindUndeclaredRouteParameter,
				Message: fmt.Sprintf("with-clause entry %q uses a dotted path; only bare resolver-result names are supported in the runtime skeleton", ref.Name),
				Span:    ref.Span(),
			})
			continue
		}
		with = append(with, ref.Name)
	}

	if len(entries) > 0 {
		return nil, entries
	}
	return &formatStep{template: s.Template(), fn: fn, with: with}, nil
}

// routeParamNames returns the names declared by every parameter
// segment in pattern, in declaration order.
func routeParamNames(pattern *ast.RoutePattern) []string {
	if pattern == nil {
		return nil
	}
	var names []string
	for _, seg := range pattern.Segments {
		if p, ok := seg.(*ast.ParameterSegment); ok {
			names = append(names, p.Name)
		}
	}
	return names
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// match returns the compiled route, bound parameters, and Allow
// header methods for a request. Three outcome shapes:
//
//   - route != nil: a handler matched the method and path; params
//     contains the bound route parameters and allow is nil.
//   - route == nil and allow != nil: the path matched some handler
//     but not for this method; allow is the sorted list of methods
//     that do match (used to construct a 405 response).
//   - route == nil and allow == nil: no handler matched the path
//     (404).
func (t *routingTable) match(method, path string) (*compiledRoute, Params, []string) {
	if t == nil {
		return nil, Params{}, nil
	}
	reqSegs := splitPath(path)

	// First try the requested method.
	if routes, ok := t.byMethod[method]; ok {
		for _, r := range routes {
			if values, matched := matchSegments(r.segments, reqSegs); matched {
				return r, Params{values: values}, nil
			}
		}
	}

	// Fall back to a path-only scan to compute Allow.
	allow := []string{}
	allowSeen := map[string]struct{}{}
	for _, m := range t.methods {
		if m == method {
			continue
		}
		for _, r := range t.byMethod[m] {
			if _, matched := matchSegments(r.segments, reqSegs); matched {
				if _, dup := allowSeen[m]; !dup {
					allowSeen[m] = struct{}{}
					allow = append(allow, m)
				}
				break
			}
		}
	}
	if len(allow) == 0 {
		return nil, Params{}, nil
	}
	sort.Strings(allow)
	return nil, Params{}, allow
}

// matchSegments compares routeSegs against reqSegs. Returns the
// bound parameter map and true when every segment matches.
//
// Segment-count strictness produces the trailing-slash policy
// automatically: a request to /users/ produces ["users", ""] which
// does not equal a route declared as ["users"].
func matchSegments(routeSegs []ast.RouteSegment, reqSegs []string) (map[string]string, bool) {
	if len(routeSegs) != len(reqSegs) {
		return nil, false
	}
	var bound map[string]string
	for i, seg := range routeSegs {
		switch s := seg.(type) {
		case *ast.LiteralSegment:
			if s.Text != reqSegs[i] {
				return nil, false
			}
		case *ast.ParameterSegment:
			if reqSegs[i] == "" {
				// An empty segment value (e.g., `/users//42`)
				// cannot bind a parameter; treat it as a miss.
				return nil, false
			}
			if bound == nil {
				bound = make(map[string]string)
			}
			bound[s.Name] = reqSegs[i]
		default:
			// WildcardSegment is not allowed in handler routes per
			// spec 001. If one appears here the AST is malformed;
			// treat as no match defensively.
			return nil, false
		}
	}
	return bound, true
}

// splitPath divides an HTTP request path into segments. A leading
// slash is consumed; the empty-string segment that would otherwise
// appear at index 0 is dropped. Segment-count strictness preserves
// trailing slashes: "/users/" → ["users", ""].
func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	if path[0] == '/' {
		path = path[1:]
	}
	if path == "" {
		// The root path "/" parses to an empty slice.
		return nil
	}
	return strings.Split(path, "/")
}
