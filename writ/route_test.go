package writ

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
	"github.com/stonean/writ/parser"
	"github.com/stonean/writ/pipeline"
)

// mustElaborate parses src and elaborates the result, failing the
// test on any parser or elaborator error. Returns the *Resolved
// suitable for feeding compileRoutes.
func mustElaborate(t *testing.T, src string) *pipeline.Resolved {
	t.Helper()
	prog, perrs := parser.ParseString("p.writ", src)
	if len(perrs) != 0 {
		t.Fatalf("parser errors: %v", perrs)
	}
	resolved, errs := pipeline.Elaborate(prog)
	if len(errs) != 0 {
		t.Fatalf("elaborator errors: %v", errs)
	}
	return resolved
}

// span builds a synthetic span on a single source for tests that
// only inspect line/column ordering.
func testSpan(src *ast.Source, line int) ast.Span {
	pos := ast.Position{Source: src, Line: line, Column: 1, Offset: line}
	return ast.Span{Start: pos, End: pos}
}

func testSrc(path string) *ast.Source {
	return &ast.Source{Path: path}
}

// litSeg / paramSeg are concise constructors for route segments.
func litSeg(src *ast.Source, line int, text string) ast.RouteSegment {
	return ast.NewLiteralSegment(testSpan(src, line), text)
}

func paramSeg(src *ast.Source, line int, name string) ast.RouteSegment {
	return ast.NewParameterSegment(testSpan(src, line), name)
}

// makeTable builds a routing table from a list of (method, segments)
// pairs. Compiled routes carry a no-op formatter and no resolves so
// the table is sufficient for exercising match() in isolation.
func makeTable(t *testing.T, src *ast.Source, defs ...routeDef) *routingTable {
	t.Helper()
	tbl := &routingTable{byMethod: make(map[string][]*compiledRoute)}
	seen := map[string]struct{}{}
	for _, d := range defs {
		r := &compiledRoute{
			method:   d.method,
			segments: d.segments,
			format:   formatStep{template: "noop", fn: noopFormatter},
			span:     testSpan(src, 1),
		}
		tbl.byMethod[d.method] = append(tbl.byMethod[d.method], r)
		if _, ok := seen[d.method]; !ok {
			seen[d.method] = struct{}{}
			tbl.methods = append(tbl.methods, d.method)
		}
	}
	// Methods are sorted in compileRoutes after construction; mirror
	// that here so the test fixture matches the production shape.
	sortStrings(tbl.methods)
	return tbl
}

type routeDef struct {
	method   string
	segments []ast.RouteSegment
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// --- splitPath ---

func TestSplitPathRoot(t *testing.T) {
	if got := splitPath("/"); len(got) != 0 {
		t.Errorf("splitPath(/) = %v, want empty", got)
	}
}

func TestSplitPathSimple(t *testing.T) {
	got := splitPath("/users/42")
	want := []string{"users", "42"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitPath(/users/42) = %v, want %v", got, want)
	}
}

func TestSplitPathTrailingSlashKeepsEmptySegment(t *testing.T) {
	got := splitPath("/users/")
	want := []string{"users", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitPath(/users/) = %v, want %v (empty segment preserved for strict matching)",
			got, want)
	}
}

// --- match: literal and parameter binding ---

func TestMatchLiteralOnly(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users")}},
	)
	r, params, allow := tbl.match("GET", "/users")
	if r == nil {
		t.Fatalf("expected match for /users, got nil")
	}
	if len(allow) != 0 {
		t.Errorf("allow = %v, want empty on hit", allow)
	}
	if params.Has("anything") {
		t.Errorf("Params should be empty for literal-only route")
	}
}

func TestMatchParameterBinds(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{
			litSeg(src, 1, "users"),
			paramSeg(src, 1, "id"),
		}},
	)
	r, params, _ := tbl.match("GET", "/users/42")
	if r == nil {
		t.Fatalf("expected match for /users/42")
	}
	if got := params.String("id"); got != "42" {
		t.Errorf("params.id = %q, want %q", got, "42")
	}
}

func TestMatchSegmentCountMismatch(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{
			litSeg(src, 1, "users"),
			paramSeg(src, 1, "id"),
		}},
	)
	r, _, allow := tbl.match("GET", "/users/42/posts")
	if r != nil {
		t.Errorf("expected no match for longer path, got %+v", r)
	}
	if len(allow) != 0 {
		t.Errorf("allow = %v, want empty for total miss", allow)
	}
}

func TestMatchTrailingSlashIsStrict(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users")}},
	)
	r, _, allow := tbl.match("GET", "/users/")
	if r != nil {
		t.Errorf("expected no match for /users/ against /users (strict), got %+v", r)
	}
	if len(allow) != 0 {
		t.Errorf("allow = %v, want empty for total miss", allow)
	}
}

// --- match: 405 with sorted Allow ---

func TestMatchMethodMismatchReturnsAllow(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users")}},
	)
	r, _, allow := tbl.match("POST", "/users")
	if r != nil {
		t.Errorf("expected method miss, got route %+v", r)
	}
	if !reflect.DeepEqual(allow, []string{"GET"}) {
		t.Errorf("allow = %v, want [GET]", allow)
	}
}

func TestMatchMultiMethodAllowSorted(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users"), paramSeg(src, 1, "id")}},
		routeDef{"PUT", []ast.RouteSegment{litSeg(src, 2, "users"), paramSeg(src, 2, "id")}},
		routeDef{"DELETE", []ast.RouteSegment{litSeg(src, 3, "users"), paramSeg(src, 3, "id")}},
	)
	r, _, allow := tbl.match("PATCH", "/users/42")
	if r != nil {
		t.Errorf("expected method miss for PATCH, got %+v", r)
	}
	want := []string{"DELETE", "GET", "PUT"}
	if !reflect.DeepEqual(allow, want) {
		t.Errorf("allow = %v, want %v (alphabetical)", allow, want)
	}
}

func TestMatchTotalMissReturnsNil(t *testing.T) {
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users")}},
	)
	r, _, allow := tbl.match("GET", "/nope")
	if r != nil || allow != nil {
		t.Errorf("expected (nil, nil) for total miss; got route=%+v allow=%v", r, allow)
	}
}

// --- match: literal precedence over parameter binding ---

func TestMatchLiteralBeatsParameterAtSamePosition(t *testing.T) {
	// /users/me declared before /users/:id should win for /users/me;
	// declaration order is the tie-break rule (first match wins).
	src := testSrc("p.writ")
	tbl := makeTable(t, src,
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 1, "users"), litSeg(src, 1, "me")}},
		routeDef{"GET", []ast.RouteSegment{litSeg(src, 2, "users"), paramSeg(src, 2, "id")}},
	)
	r, params, _ := tbl.match("GET", "/users/me")
	if r == nil {
		t.Fatalf("expected match for /users/me")
	}
	if params.Has("id") {
		t.Errorf("/users/me should bind no parameters when literal route declared first; got id=%q", params.String("id"))
	}
}

// noopFormatter is a placeholder used by route tests; dispatch tests
// in task 8 will exercise real formatter behavior. Used only to make
// the compiledRoute fixture well-formed.
func noopFormatter(_ context.Context, _ http.ResponseWriter, _ Results) error { return nil }

func noopResolver(_ context.Context, _ Params) (any, error) { return nil, nil }

// --- compileRoutes ---

func TestCompileRoutesHappyPath(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	tbl, entries := compileRoutes(resolved, resolvers, formatters, nil, nil)
	if len(entries) != 0 {
		t.Fatalf("compileRoutes returned entries: %v", entries)
	}
	if got := len(tbl.byMethod["GET"]); got != 1 {
		t.Fatalf("byMethod[GET] = %d routes, want 1", got)
	}
	r := tbl.byMethod["GET"][0]
	if r.method != "GET" {
		t.Errorf("method = %q, want GET", r.method)
	}
	if got := len(r.resolves); got != 1 || r.resolves[0].name != "user" {
		t.Errorf("resolves = %+v, want one step named user", r.resolves)
	}
	if r.format.template != "user.show" {
		t.Errorf("format.template = %q, want user.show", r.format.template)
	}
	if got := r.resolves[0].paramArgs; len(got) != 1 || got[0] != "id" {
		t.Errorf("paramArgs = %v, want [id]", got)
	}
}

func TestCompileRoutesUnregisteredResolverEntry(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := compileRoutes(resolved, nil, formatters, nil, nil)
	if len(entries) != 1 || entries[0].Kind != KindUnregisteredResolver {
		t.Fatalf("entries = %v, want one KindUnregisteredResolver", entries)
	}
}

func TestCompileRoutesUnregisteredFormatterEntry(t *testing.T) {
	src := `GET /users/:id ->
  format user.show with user
`
	resolved := mustElaborate(t, src)

	_, entries := compileRoutes(resolved, nil, nil, nil, nil)
	if len(entries) != 1 || entries[0].Kind != KindUnregisteredFormatter {
		t.Fatalf("entries = %v, want one KindUnregisteredFormatter", entries)
	}
}

func TestCompileRoutesUndeclaredRouteParameter(t *testing.T) {
	src := `GET /users/:id ->
  resolve other = db.other(:bogus)
  format user.show with other
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.other": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := compileRoutes(resolved, resolvers, formatters, nil, nil)
	if len(entries) != 1 || entries[0].Kind != KindUndeclaredRouteParameter {
		t.Fatalf("entries = %v, want one KindUndeclaredRouteParameter", entries)
	}
}

func TestCompileRoutesNonRouteParamArgIsRejected(t *testing.T) {
	// `limit=10` is a NamedArg literal — out of scope per the
	// skeleton; reported under KindUndeclaredRouteParameter with a
	// message explaining the constraint.
	src := `GET /users ->
  resolve users = db.users(limit=10)
  format users.list with users
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"users.list": noopFormatter}

	_, entries := compileRoutes(resolved, resolvers, formatters, nil, nil)
	if len(entries) != 1 || entries[0].Kind != KindUndeclaredRouteParameter {
		t.Fatalf("entries = %v, want one KindUndeclaredRouteParameter for non-:name arg", entries)
	}
}

// noopErrorFormatter is a placeholder error formatter used by the
// compiled-error-entry tests. The dispatch tests (task 7) exercise
// real error-formatter behavior end-to-end.
func noopErrorFormatter(_ context.Context, _ http.ResponseWriter, _ ErrorData) error { return nil }

func TestCompileRoutesEmptyErrorMap(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	tbl, entries := compileRoutes(resolved, resolvers, formatters, nil, nil)
	if len(entries) != 0 {
		t.Fatalf("compileRoutes returned entries: %v", entries)
	}
	r := tbl.byMethod["GET"][0]
	if len(r.errorEntries) != 0 {
		t.Errorf("errorEntries = %v, want empty for handler with no errors block", r.errorEntries)
	}
}

func TestCompileRoutesConcreteTypeErrorEntry(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorFormatters := map[string]ErrorFormatterFunc{"notFoundJSON": noopErrorFormatter}
	errorTypes := map[string]func(error) bool{"NotFound": func(error) bool { return true }}

	tbl, entries := compileRoutes(resolved, resolvers, formatters, errorFormatters, errorTypes)
	if len(entries) != 0 {
		t.Fatalf("compileRoutes returned entries: %v", entries)
	}
	r := tbl.byMethod["GET"][0]
	if got := len(r.errorEntries); got != 1 {
		t.Fatalf("errorEntries len = %d, want 1", got)
	}
	if r.errorEntries[0].typeName != "NotFound" {
		t.Errorf("typeName = %q, want NotFound", r.errorEntries[0].typeName)
	}
	if r.errorEntries[0].isDefault {
		t.Errorf("isDefault = true, want false for concrete entry")
	}
	if r.errorEntries[0].matcher == nil {
		t.Errorf("matcher = nil, want non-nil for concrete entry")
	}
	if r.errorEntries[0].formatter == nil {
		t.Errorf("formatter = nil, want non-nil")
	}
}

func TestCompileRoutesDefaultErrorEntry(t *testing.T) {
	src := `errors /users/* ->
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorFormatters := map[string]ErrorFormatterFunc{"defaultJSON": noopErrorFormatter}

	tbl, entries := compileRoutes(resolved, resolvers, formatters, errorFormatters, nil)
	if len(entries) != 0 {
		t.Fatalf("compileRoutes returned entries: %v", entries)
	}
	r := tbl.byMethod["GET"][0]
	if got := len(r.errorEntries); got != 1 {
		t.Fatalf("errorEntries len = %d, want 1", got)
	}
	if !r.errorEntries[0].isDefault {
		t.Errorf("isDefault = false, want true for default entry")
	}
	if r.errorEntries[0].matcher != nil {
		t.Errorf("matcher = non-nil, want nil for default entry")
	}
	if r.errorEntries[0].formatter == nil {
		t.Errorf("formatter = nil, want non-nil")
	}
}

func TestCompileRoutesMixedErrorEntriesPreserveOrder(t *testing.T) {
	// Spec 002 documents most-specific-first ordering. The runtime
	// preserves order verbatim from pipeline.Handler.ErrorMap; this
	// test pins index-by-index equality.
	src := `errors /users/* ->
  NotFound notFoundJSON
  Validation validationJSON
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorFormatters := map[string]ErrorFormatterFunc{
		"notFoundJSON":   noopErrorFormatter,
		"validationJSON": noopErrorFormatter,
		"defaultJSON":    noopErrorFormatter,
	}
	errorTypes := map[string]func(error) bool{
		"NotFound":   func(error) bool { return false },
		"Validation": func(error) bool { return false },
	}

	tbl, entries := compileRoutes(resolved, resolvers, formatters, errorFormatters, errorTypes)
	if len(entries) != 0 {
		t.Fatalf("compileRoutes returned entries: %v", entries)
	}
	r := tbl.byMethod["GET"][0]
	if got := len(r.errorEntries); got != 3 {
		t.Fatalf("errorEntries len = %d, want 3", got)
	}
	wantNames := []string{"NotFound", "Validation", "default"}
	wantIsDefault := []bool{false, false, true}
	for i, want := range wantNames {
		if got := r.errorEntries[i].typeName; got != want {
			t.Errorf("entry[%d].typeName = %q, want %q", i, got, want)
		}
		if got := r.errorEntries[i].isDefault; got != wantIsDefault[i] {
			t.Errorf("entry[%d].isDefault = %v, want %v", i, got, wantIsDefault[i])
		}
	}
}
