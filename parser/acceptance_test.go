package parser

import (
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stonean/writ/ast"
)

// acceptance_test.go covers the spec.md acceptance criteria as
// directly as possible. Tests are grouped by spec section so a
// reader can audit coverage by reading top-to-bottom against the
// spec.

// ===== Constructs and Containment =====

func TestAcceptanceFullProgramContainsEveryConstruct(t *testing.T) {
	src := `system ->
  log :id

group /admin/* ->
  approve auth.isAdmin

errors /admin/* ->
  NotFound notFoundJSON
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show.html with user

POST /users ->
  commit user = db.users.create(body CreateUser)
  redirect /users/:user.id
`
	prog, errs := ParseString("test.writ", src)
	mustNoErrors(t, errs)
	if prog.System == nil {
		t.Errorf("missing system block")
	}
	if len(prog.Groups) != 1 {
		t.Errorf("groups = %d, want 1", len(prog.Groups))
	}
	if len(prog.Errors) != 1 {
		t.Errorf("errors blocks = %d, want 1", len(prog.Errors))
	}
	if len(prog.Handlers) != 2 {
		t.Errorf("handlers = %d, want 2", len(prog.Handlers))
	}
}

func TestAcceptanceContextualKeywordsAsIdentifierSegments(t *testing.T) {
	// Every reserved word from the README's "DSL Syntax" should also
	// be valid as an identifier segment in any identifier position.
	keywords := []string{
		"system", "group", "errors", "include",
		"log", "measure", "session", "csrf", "limit", "approve",
		"resolve", "commit", "emit", "format", "redirect", "layout",
		"none", "OR", "AND", "NOT", "with", "using", "body", "query",
		"default",
	}
	for _, kw := range keywords {
		t.Run(kw, func(t *testing.T) {
			src := fmt.Sprintf("system ->\n  resolve x = db.%s.refresh()\n", kw)
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			r, ok := prog.System.Statements[0].(*ast.ResolveStmt)
			if !ok {
				t.Fatalf("stmt = %T, want ResolveStmt", prog.System.Statements[0])
			}
			want := "db." + kw + ".refresh"
			if r.Call.Name != want {
				t.Errorf("call name = %q, want %q", r.Call.Name, want)
			}
		})
	}
}

func TestAcceptanceUppercaseMethodsAccepted(t *testing.T) {
	methods := []string{
		"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS",
		"MKCOL", "PROPFIND", "PURGE", "M-SEARCH",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			src := m + " /things ->\n  log :id\n"
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			if len(prog.Handlers) != 1 {
				t.Fatalf("handlers = %d, want 1", len(prog.Handlers))
			}
			if prog.Handlers[0].Method != m {
				t.Errorf("method = %q, want %q", prog.Handlers[0].Method, m)
			}
		})
	}
}

func TestAcceptanceLowercaseMethodIsRejected(t *testing.T) {
	cases := []string{"get", "post", "Get", "gET", "options"}
	for _, m := range cases {
		t.Run(m, func(t *testing.T) {
			_, errs := ParseString("test.writ", m+" /users ->\n  log :id\n")
			if len(errs) == 0 {
				t.Errorf("expected error for %q", m)
			}
		})
	}
}

func TestAcceptanceEmptyBlocksAreErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"system bare", "system ->\n"},
		{"system blank lines", "system ->\n\n\n"},
		{"system comments only", "system ->\n  # one\n  # two\n"},
		{"group", "group /admin/* ->\n"},
		{"errors", "errors /admin/* ->\n"},
		{"handler", "GET /users ->\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ParseString("test.writ", tc.src)
			if len(errs) == 0 {
				t.Fatalf("expected empty-block error")
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Message, "empty") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected an 'empty ...' error, got %v", errs)
			}
		})
	}
}

// ===== Lexical Forms =====

func TestAcceptanceIdentifierGrammarPositive(t *testing.T) {
	cases := []string{
		"a",
		"abc",
		"a1",
		"a_b",
		"camelCase",
		"PascalCase",
		"a.b",
		"a.b.c.d",
		"db_users.create_one",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			src := fmt.Sprintf("system ->\n  resolve x = %s()\n", id)
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			r := prog.System.Statements[0].(*ast.ResolveStmt)
			if r.Call.Name != id {
				t.Errorf("name = %q, want %q", r.Call.Name, id)
			}
		})
	}
}

func TestAcceptanceIdentifierGrammarNegative(t *testing.T) {
	// Inputs that must produce a parse error somewhere — leading
	// digit, dash inside identifier, leading dot, consecutive dots.
	cases := []struct {
		name string
		src  string
	}{
		{"leading digit", "system ->\n  resolve x = 1abc()\n"},
		{"dash inside name", "system ->\n  resolve x = a-b()\n"},
		{"leading dot", "system ->\n  resolve x = .abc()\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ParseString("test.writ", tc.src)
			if len(errs) == 0 {
				t.Fatalf("expected error for %q", tc.src)
			}
		})
	}
}

func TestAcceptanceIntegerLiteralPositive(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"60", 60},
		{"-1", -1},
		{"-100", -100},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			src := fmt.Sprintf("system ->\n  resolve x = db.f(%s)\n", tc.input)
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			c := prog.System.Statements[0].(*ast.ResolveStmt).Call
			lit, ok := c.Args[0].(*ast.IntLit)
			if !ok {
				t.Fatalf("arg = %T, want IntLit", c.Args[0])
			}
			if lit.Value != tc.want {
				t.Errorf("value = %d, want %d", lit.Value, tc.want)
			}
		})
	}
}

func TestAcceptanceIntegerLiteralNegative(t *testing.T) {
	// Anything outside `-?[0-9]+` must error: hex, underscores,
	// floats, scientific. (`007` is valid per the grammar; we don't
	// test it as a rejection.)
	cases := []struct {
		name string
		src  string
	}{
		{"hex", "system ->\n  resolve x = db.f(0x10)\n"},
		{"underscore", "system ->\n  resolve x = db.f(1_000)\n"},
		{"float", "system ->\n  resolve x = db.f(1.5)\n"},
		{"scientific", "system ->\n  resolve x = db.f(1e3)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ParseString("test.writ", tc.src)
			if len(errs) == 0 {
				t.Fatalf("expected error for %q", tc.src)
			}
		})
	}
}

func TestAcceptanceStringLiteralEscapes(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`""`, ""},
		{`"hello"`, "hello"},
		{`"\""`, `"`},
		{`"\\"`, `\`},
		{`"\n"`, "\n"},
		{`"\t"`, "\t"},
		{`"\r"`, "\r"},
		{`"a\nb"`, "a\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			src := fmt.Sprintf("system ->\n  resolve x = db.f(s=%s)\n", tc.input)
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			c := prog.System.Statements[0].(*ast.ResolveStmt).Call
			na := c.Args[0].(*ast.NamedArg)
			s := na.Value.(*ast.StringLit)
			if s.Value != tc.want {
				t.Errorf("value = %q, want %q", s.Value, tc.want)
			}
		})
	}
}

func TestAcceptanceStringLiteralRejections(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"unterminated", `system ->` + "\n  log \"hello\n"},
		{"raw newline", `system ->` + "\n  log \"hello\nworld\"\n"},
		{"unknown escape", `system ->` + "\n  log \"\\q\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ParseString("test.writ", tc.src)
			if len(errs) == 0 {
				t.Fatalf("expected error for %q", tc.src)
			}
		})
	}
}

func TestAcceptanceRateLiteralUnits(t *testing.T) {
	for _, unit := range []string{"sec", "min", "hour", "day"} {
		t.Run(unit, func(t *testing.T) {
			src := fmt.Sprintf("system ->\n  limit rate.ip(60/%s)\n", unit)
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			r := prog.System.Statements[0].(*ast.LimitStmt).Call.Args[0].(*ast.RateLit)
			if r.Count != 60 || r.Unit != unit {
				t.Errorf("rate = %d/%s, want 60/%s", r.Count, r.Unit, unit)
			}
		})
	}
}

func TestAcceptanceRateLiteralBadUnits(t *testing.T) {
	cases := []string{"foo", "second", "minute", "ms", "h", "s", "m"}
	for _, unit := range cases {
		t.Run(unit, func(t *testing.T) {
			_, errs := ParseString("test.writ",
				fmt.Sprintf("system ->\n  limit rate.ip(60/%s)\n", unit))
			if len(errs) == 0 {
				t.Errorf("expected error for unit %q", unit)
			}
		})
	}
}

func TestAcceptanceCommentsStrippedFromAST(t *testing.T) {
	src := `# top of file
system ->         # trailing
  log :id         # inline
  # whole-line comment
  measure :id
# another top-level comment
`
	prog, errs := ParseString("test.writ", src)
	mustNoErrors(t, errs)
	if len(prog.System.Statements) != 2 {
		t.Fatalf("stmts = %d, want 2", len(prog.System.Statements))
	}
}

func TestAcceptanceNoBlockCommentForm(t *testing.T) {
	// `/* */` is not recognized; the `/` and `*` tokens leak to the
	// parser and produce an error.
	_, errs := ParseString("test.writ", "/* not a comment */\nsystem ->\n  log :id\n")
	if len(errs) == 0 {
		t.Fatalf("expected error: block comment syntax must not parse")
	}
}

func TestAcceptanceIndentationPermissive(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"two spaces", "system ->\n  log :id\n"},
		{"four spaces", "system ->\n    log :id\n"},
		{"single tab", "system ->\n\tlog :id\n"},
		{"two tabs", "system ->\n\t\tlog :id\n"},
		{"mixed tab+space", "system ->\n\t  log :id\n"},
		{"single space", "system ->\n log :id\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, errs := ParseString("test.writ", tc.src)
			mustNoErrors(t, errs)
			if len(prog.System.Statements) != 1 {
				t.Fatalf("stmts = %d, want 1", len(prog.System.Statements))
			}
		})
	}
}

// ===== Approve Expressions =====

func TestAcceptanceApproveDeeplyNestedParens(t *testing.T) {
	// (((NOT a) AND (b OR c)) AND d)
	prog, errs := ParseString("test.writ",
		"system ->\n  approve (((NOT a) AND (b OR c)) AND d)\n")
	mustNoErrors(t, errs)
	expr := prog.System.Statements[0].(*ast.ApproveStmt).Expr
	outer, ok := expr.(*ast.ApproveAnd)
	if !ok {
		t.Fatalf("outer = %T, want ApproveAnd", expr)
	}
	if _, ok := outer.Right.(*ast.ApproveCall); !ok {
		t.Fatalf("outer.Right = %T, want ApproveCall(d)", outer.Right)
	}
	innerAnd, ok := outer.Left.(*ast.ApproveAnd)
	if !ok {
		t.Fatalf("outer.Left = %T, want ApproveAnd", outer.Left)
	}
	if _, ok := innerAnd.Left.(*ast.ApproveNot); !ok {
		t.Fatalf("innerAnd.Left = %T, want ApproveNot", innerAnd.Left)
	}
	if _, ok := innerAnd.Right.(*ast.ApproveOr); !ok {
		t.Fatalf("innerAnd.Right = %T, want ApproveOr", innerAnd.Right)
	}
}

func TestAcceptanceApproveLeftAssociativity(t *testing.T) {
	// a AND b AND c → (a AND b) AND c
	prog, errs := ParseString("test.writ",
		"system ->\n  approve a AND b AND c\n")
	mustNoErrors(t, errs)
	top := prog.System.Statements[0].(*ast.ApproveStmt).Expr.(*ast.ApproveAnd)
	if _, ok := top.Left.(*ast.ApproveAnd); !ok {
		t.Errorf("top.Left = %T, want ApproveAnd (left-assoc)", top.Left)
	}
	if _, ok := top.Right.(*ast.ApproveCall); !ok {
		t.Errorf("top.Right = %T, want ApproveCall", top.Right)
	}
}

func TestAcceptanceApproveNotRightAssociativity(t *testing.T) {
	// NOT NOT a → NOT (NOT a)
	prog, errs := ParseString("test.writ",
		"system ->\n  approve NOT NOT a\n")
	mustNoErrors(t, errs)
	top := prog.System.Statements[0].(*ast.ApproveStmt).Expr.(*ast.ApproveNot)
	if _, ok := top.Inner.(*ast.ApproveNot); !ok {
		t.Errorf("top.Inner = %T, want ApproveNot", top.Inner)
	}
}

// ===== Routes =====

func TestAcceptanceRouteSegmentLiterals(t *testing.T) {
	// Verify literal grammar: letters, digits, underscore, dash.
	cases := []struct {
		pattern  string
		literals []string
	}{
		{"/users", []string{"users"}},
		{"/v1", []string{"v1"}},
		{"/v1-2", []string{"v1-2"}},
		{"/things_2", []string{"things_2"}},
		{"/abc/def-ghi/jkl_2", []string{"abc", "def-ghi", "jkl_2"}},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			src := "GET " + tc.pattern + " ->\n  log :id\n"
			prog, errs := ParseString("test.writ", src)
			mustNoErrors(t, errs)
			pat := prog.Handlers[0].Pattern
			if len(pat.Segments) != len(tc.literals) {
				t.Fatalf("segments = %d, want %d", len(pat.Segments), len(tc.literals))
			}
			for i, seg := range pat.Segments {
				lit, ok := seg.(*ast.LiteralSegment)
				if !ok {
					t.Fatalf("seg[%d] = %T, want LiteralSegment", i, seg)
				}
				if lit.Text != tc.literals[i] {
					t.Errorf("seg[%d] = %q, want %q", i, lit.Text, tc.literals[i])
				}
			}
		})
	}
}

func TestAcceptanceRouteRootPattern(t *testing.T) {
	prog, errs := ParseString("test.writ", "GET / ->\n  log :id\n")
	mustNoErrors(t, errs)
	if len(prog.Handlers[0].Pattern.Segments) != 0 {
		t.Errorf("root pattern segments = %d, want 0",
			len(prog.Handlers[0].Pattern.Segments))
	}
}

// ===== Errors and Recovery =====

func TestAcceptanceParseErrorCarriesFileLineColumnMessage(t *testing.T) {
	_, errs := ParseString("custom.writ", "GET\n")
	if len(errs) == 0 {
		t.Fatalf("expected error")
	}
	e := errs[0]
	if e.File != "custom.writ" {
		t.Errorf("File = %q, want custom.writ", e.File)
	}
	if e.Line < 1 || e.Column < 1 {
		t.Errorf("Line/Column not 1-based: %d:%d", e.Line, e.Column)
	}
	if e.Message == "" {
		t.Errorf("Message is empty")
	}
	want := "custom.writ:1:"
	if !strings.HasPrefix(e.Error(), want) {
		t.Errorf("Error() = %q, want prefix %q", e.Error(), want)
	}
}

// ===== Source Locations =====

// walkProgram visits every Node reachable from the program and
// invokes visit. A returned non-nil error halts the walk.
func walkProgram(prog *ast.Program, visit func(ast.Node) error) error {
	if prog == nil {
		return nil
	}
	if err := visit(prog); err != nil {
		return err
	}
	if prog.System != nil {
		if err := visit(prog.System); err != nil {
			return err
		}
		for _, s := range prog.System.Statements {
			if err := walkStmt(s, visit); err != nil {
				return err
			}
		}
	}
	for _, g := range prog.Groups {
		if err := visit(g); err != nil {
			return err
		}
		if err := walkRoutePattern(g.Pattern, visit); err != nil {
			return err
		}
		for _, s := range g.Statements {
			if err := walkStmt(s, visit); err != nil {
				return err
			}
		}
	}
	for _, eb := range prog.Errors {
		if err := visit(eb); err != nil {
			return err
		}
		if err := walkRoutePattern(eb.Pattern, visit); err != nil {
			return err
		}
		for _, en := range eb.Entries {
			if err := visit(en); err != nil {
				return err
			}
		}
	}
	for _, h := range prog.Handlers {
		if err := visit(h); err != nil {
			return err
		}
		if err := walkRoutePattern(h.Pattern, visit); err != nil {
			return err
		}
		for _, s := range h.Statements {
			if err := walkStmt(s, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func walkRoutePattern(pat *ast.RoutePattern, visit func(ast.Node) error) error {
	if pat == nil {
		return nil
	}
	if err := visit(pat); err != nil {
		return err
	}
	for _, seg := range pat.Segments {
		if err := visit(seg); err != nil {
			return err
		}
	}
	return nil
}

func walkStmt(s ast.Stmt, visit func(ast.Node) error) error {
	if s == nil {
		return nil
	}
	if err := visit(s); err != nil {
		return err
	}
	switch v := s.(type) {
	case *ast.LogStmt:
		for _, a := range v.Args {
			if err := walkExpr(a, visit); err != nil {
				return err
			}
		}
	case *ast.MeasureStmt:
		for _, a := range v.Args {
			if err := walkExpr(a, visit); err != nil {
				return err
			}
		}
	case *ast.LimitStmt:
		if err := walkCall(v.Call, visit); err != nil {
			return err
		}
	case *ast.ApproveStmt:
		if err := walkApprove(v.Expr, visit); err != nil {
			return err
		}
	case *ast.ResolveStmt:
		if err := walkCall(v.Call, visit); err != nil {
			return err
		}
	case *ast.CommitStmt:
		if err := walkCall(v.Call, visit); err != nil {
			return err
		}
	case *ast.FormatStmt:
		// FormatStmt.Data is []NamedRef (value) — no separate visit.
	}
	return nil
}

func walkExpr(e ast.Expr, visit func(ast.Node) error) error {
	if e == nil {
		return nil
	}
	if err := visit(e); err != nil {
		return err
	}
	if na, ok := e.(*ast.NamedArg); ok && na.Value != nil {
		if err := visit(na.Value); err != nil {
			return err
		}
	}
	return nil
}

func walkCall(c *ast.Call, visit func(ast.Node) error) error {
	if c == nil {
		return nil
	}
	if err := visit(c); err != nil {
		return err
	}
	for _, a := range c.Args {
		if err := walkExpr(a, visit); err != nil {
			return err
		}
	}
	return nil
}

func walkApprove(e ast.ApproveExpr, visit func(ast.Node) error) error {
	if e == nil {
		return nil
	}
	if err := visit(e); err != nil {
		return err
	}
	switch v := e.(type) {
	case *ast.ApproveOr:
		if err := walkApprove(v.Left, visit); err != nil {
			return err
		}
		if err := walkApprove(v.Right, visit); err != nil {
			return err
		}
	case *ast.ApproveAnd:
		if err := walkApprove(v.Left, visit); err != nil {
			return err
		}
		if err := walkApprove(v.Right, visit); err != nil {
			return err
		}
	case *ast.ApproveNot:
		return walkApprove(v.Inner, visit)
	case *ast.ApproveCall:
		return walkCall(v.Call, visit)
	}
	return nil
}

func TestAcceptanceEverySpanReferencesOriginatingSource(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
include admin.writ
GET /home ->
  format home.html with user
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
errors /admin/* ->
  NotFound notFoundJSON
`)},
	}
	prog, errs := Parse("app.writ", WithFS(fsys))
	mustNoErrors(t, errs)

	// Source.Path → expected file for nodes originating in that file.
	err := walkProgram(prog, func(n ast.Node) error {
		span := n.Span()
		if span.Start.Source == nil {
			return fmt.Errorf("node %T has nil Source", n)
		}
		if span.Start.Source != span.End.Source {
			return fmt.Errorf("node %T spans across sources", n)
		}
		if span.Start.Offset > span.End.Offset {
			return fmt.Errorf("node %T has inverted span", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// The group came from admin.writ; the handler from app.writ.
	if prog.Groups[0].Span().Start.Source.Path != "admin.writ" {
		t.Errorf("group span source = %q, want admin.writ",
			prog.Groups[0].Span().Start.Source.Path)
	}
	if prog.Handlers[0].Span().Start.Source.Path != "app.writ" {
		t.Errorf("handler span source = %q, want app.writ",
			prog.Handlers[0].Span().Start.Source.Path)
	}
}

func TestAcceptanceSpanTextRoundTripsForBlockHeaders(t *testing.T) {
	src := "GET /users/:id ->\n  log :id\n"
	prog, errs := ParseString("test.writ", src)
	mustNoErrors(t, errs)
	h := prog.Handlers[0]

	// Method span text equals "GET".
	got := string(h.MethodSpan.Text())
	if got != "GET" {
		t.Errorf("MethodSpan.Text() = %q, want %q", got, "GET")
	}

	// Pattern span text equals "/users/:id".
	got = string(h.Pattern.Span().Text())
	if got != "/users/:id" {
		t.Errorf("Pattern Span.Text() = %q, want %q", got, "/users/:id")
	}

	// Statement span text equals "log :id".
	got = string(h.Statements[0].Span().Text())
	if got != "log :id" {
		t.Errorf("Statement Span.Text() = %q, want %q", got, "log :id")
	}
}

// ===== Determinism (Task 10) =====

// nodeDigest is a stable, structural representation of an AST node
// keyed by its Go type name and span coordinates. Two parses of the
// same input produce the same digest.
type nodeDigest struct {
	Kind   string
	Path   string
	Line   int
	Column int
	Offset int
	EndOff int
}

func collectDigests(t *testing.T, prog *ast.Program) []nodeDigest {
	t.Helper()
	var out []nodeDigest
	err := walkProgram(prog, func(n ast.Node) error {
		s := n.Span()
		path := ""
		if s.Start.Source != nil {
			path = s.Start.Source.Path
		}
		out = append(out, nodeDigest{
			Kind:   reflect.TypeOf(n).String(),
			Path:   path,
			Line:   s.Start.Line,
			Column: s.Start.Column,
			Offset: s.Start.Offset,
			EndOff: s.End.Offset,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func TestAcceptanceParseTwiceProducesStructurallyEqualASTs(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
  measure :id
  session cookie
  csrf auto
  limit rate.ip(60/min)
  approve NOT a AND b OR c

include admin.writ

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show.html with user using layout main
POST /users ->
  commit user = db.users.create(body CreateUser)
  redirect /users/:user.id
errors /admin/* ->
  NotFound notFoundJSON
  default defaultJSON
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
  emit admin.access with user
`)},
	}
	a, errsA := Parse("app.writ", WithFS(fsys))
	b, errsB := Parse("app.writ", WithFS(fsys))
	if len(errsA) > 0 || len(errsB) > 0 {
		t.Fatalf("unexpected errors: A=%v B=%v", errsA, errsB)
	}
	dA := collectDigests(t, a)
	dB := collectDigests(t, b)
	if !reflect.DeepEqual(dA, dB) {
		t.Fatalf("AST digests differ between parses\nA=%+v\nB=%+v", dA, dB)
	}
}

// recordingFS wraps an fs.FS and records every path passed to Open.
type recordingFS struct {
	inner fs.FS
	mu    sync.Mutex
	opens []string
}

func (r *recordingFS) Open(name string) (fs.File, error) {
	r.mu.Lock()
	r.opens = append(r.opens, name)
	r.mu.Unlock()
	return r.inner.Open(name)
}

func (r *recordingFS) Opens() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string{}, r.opens...)
	sort.Strings(out)
	return out
}

func TestAcceptanceParseDoesNotReadOutsideIncludeGraph(t *testing.T) {
	inner := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
include admin.writ
GET /home ->
  log :id
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
`)},
		// Files the parser must not touch.
		"untouched.writ": &fstest.MapFile{Data: []byte("system ->\n  log :id\n")},
		"secret.txt":     &fstest.MapFile{Data: []byte("nope")},
	}
	rec := &recordingFS{inner: inner}
	_, errs := Parse("app.writ", WithFS(rec))
	mustNoErrors(t, errs)

	got := rec.Opens()
	want := []string{"admin.writ", "app.writ"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opened files = %v, want %v", got, want)
	}
}

func TestAcceptanceParseStringPerformsNoIO(t *testing.T) {
	// ParseString never wires a filesystem; an attempt to include
	// must surface as an error, not as a stray disk read.
	src := `system ->
  log :id
include should-fail.writ
`
	prog, errs := ParseString("inline.writ", src)
	if prog == nil {
		t.Fatalf("nil program")
	}
	if len(errs) == 0 {
		t.Fatalf("expected an include error")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "no filesystem configured") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'no filesystem configured' error, got %v", errs)
	}
}
