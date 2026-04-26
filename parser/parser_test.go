package parser

import (
	"testing"

	"github.com/stonean/writ/ast"
)

func parseStr(t *testing.T, src string) (*ast.Program, []Error) {
	t.Helper()
	prog, errs := ParseString("test.writ", src)
	if prog == nil {
		t.Fatalf("ParseString returned nil program")
	}
	return prog, errs
}

func mustNoErrors(t *testing.T, errs []Error) {
	t.Helper()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors:\n%v", errs)
	}
}

func TestParseSystemBlock(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  log :id\n")
	mustNoErrors(t, errs)
	if prog.System == nil {
		t.Fatalf("expected system block")
	}
	if len(prog.System.Statements) != 1 {
		t.Fatalf("got %d stmts, want 1", len(prog.System.Statements))
	}
	if _, ok := prog.System.Statements[0].(*ast.LogStmt); !ok {
		t.Fatalf("expected LogStmt, got %T", prog.System.Statements[0])
	}
}

func TestParseGroupBlock(t *testing.T) {
	prog, errs := parseStr(t, "group /admin/* ->\n  approve auth.isAdmin\n")
	mustNoErrors(t, errs)
	if len(prog.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(prog.Groups))
	}
	g := prog.Groups[0]
	if g.Pattern == nil || len(g.Pattern.Segments) != 2 {
		t.Fatalf("group pattern wrong: %#v", g.Pattern)
	}
	if _, ok := g.Pattern.Segments[1].(*ast.WildcardSegment); !ok {
		t.Fatalf("expected wildcard final segment")
	}
}

func TestParseHandlerBlockSimpleMethod(t *testing.T) {
	prog, errs := parseStr(t, "GET /users/:id ->\n  resolve user = db.users(:id)\n  format user.show.html with user\n")
	mustNoErrors(t, errs)
	if len(prog.Handlers) != 1 {
		t.Fatalf("got %d handlers, want 1", len(prog.Handlers))
	}
	h := prog.Handlers[0]
	if h.Method != "GET" {
		t.Fatalf("method = %q, want GET", h.Method)
	}
	if len(h.Pattern.Segments) != 2 {
		t.Fatalf("pattern segments = %d, want 2", len(h.Pattern.Segments))
	}
	if param, ok := h.Pattern.Segments[1].(*ast.ParameterSegment); !ok || param.Name != "id" {
		t.Fatalf("expected ParameterSegment(id), got %#v", h.Pattern.Segments[1])
	}
	if len(h.Statements) != 2 {
		t.Fatalf("stmts = %d, want 2", len(h.Statements))
	}
}

func TestParseHandlerBlockHyphenatedMethod(t *testing.T) {
	prog, errs := parseStr(t, "M-SEARCH /things ->\n  log :id\n")
	mustNoErrors(t, errs)
	if len(prog.Handlers) != 1 {
		t.Fatalf("got %d handlers, want 1", len(prog.Handlers))
	}
	if prog.Handlers[0].Method != "M-SEARCH" {
		t.Fatalf("method = %q, want M-SEARCH", prog.Handlers[0].Method)
	}
}

func TestParseLowercaseMethodIsError(t *testing.T) {
	_, errs := parseStr(t, "get /users ->\n  log :id\n")
	if len(errs) == 0 {
		t.Fatalf("expected error for lowercase method")
	}
}

func TestParseErrorsBlock(t *testing.T) {
	prog, errs := parseStr(t, "errors /admin/* ->\n  NotFound notFoundJSON\n  default defaultJSON\n")
	mustNoErrors(t, errs)
	if len(prog.Errors) != 1 {
		t.Fatalf("got %d errors blocks, want 1", len(prog.Errors))
	}
	eb := prog.Errors[0]
	if len(eb.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(eb.Entries))
	}
	if eb.Entries[0].TypeName != "NotFound" {
		t.Fatalf("entry 0 type = %q, want NotFound", eb.Entries[0].TypeName)
	}
	if !eb.Entries[1].IsDefault {
		t.Fatalf("entry 1 should be default")
	}
}

func TestParseEmptyBlockIsError(t *testing.T) {
	_, errs := parseStr(t, "system ->\n")
	if len(errs) == 0 {
		t.Fatalf("expected empty-block error")
	}
}

func TestParseAllStatementKinds(t *testing.T) {
	src := `system ->
  log :id
  measure :id
  session cookie
  csrf auto
  limit rate.ip(60/min)
  approve auth.authenticated
  resolve user = db.users(:id)
  commit user = db.users.create(body CreateUserInput)
  commit db.users.delete(:id)
  emit user.created with user
  format user.show.html with user using layout admin
  redirect /users/:user.id
  layout main
`
	prog, errs := parseStr(t, src)
	mustNoErrors(t, errs)
	if prog.System == nil || len(prog.System.Statements) != 13 {
		t.Fatalf("want 13 system stmts, got %d", len(prog.System.Statements))
	}

	stmts := prog.System.Statements
	wantTypes := []any{
		(*ast.LogStmt)(nil),
		(*ast.MeasureStmt)(nil),
		(*ast.SessionStmt)(nil),
		(*ast.CSRFStmt)(nil),
		(*ast.LimitStmt)(nil),
		(*ast.ApproveStmt)(nil),
		(*ast.ResolveStmt)(nil),
		(*ast.CommitStmt)(nil), // named
		(*ast.CommitStmt)(nil), // bare
		(*ast.EmitStmt)(nil),
		(*ast.FormatStmt)(nil),
		(*ast.RedirectStmt)(nil),
		(*ast.LayoutStmt)(nil),
	}
	for i, want := range wantTypes {
		switch want.(type) {
		case *ast.LogStmt:
			if _, ok := stmts[i].(*ast.LogStmt); !ok {
				t.Errorf("stmt[%d] = %T, want LogStmt", i, stmts[i])
			}
		case *ast.MeasureStmt:
			if _, ok := stmts[i].(*ast.MeasureStmt); !ok {
				t.Errorf("stmt[%d] = %T, want MeasureStmt", i, stmts[i])
			}
		case *ast.SessionStmt:
			if _, ok := stmts[i].(*ast.SessionStmt); !ok {
				t.Errorf("stmt[%d] = %T, want SessionStmt", i, stmts[i])
			}
		case *ast.CSRFStmt:
			if _, ok := stmts[i].(*ast.CSRFStmt); !ok {
				t.Errorf("stmt[%d] = %T, want CSRFStmt", i, stmts[i])
			}
		case *ast.LimitStmt:
			if _, ok := stmts[i].(*ast.LimitStmt); !ok {
				t.Errorf("stmt[%d] = %T, want LimitStmt", i, stmts[i])
			}
		case *ast.ApproveStmt:
			if _, ok := stmts[i].(*ast.ApproveStmt); !ok {
				t.Errorf("stmt[%d] = %T, want ApproveStmt", i, stmts[i])
			}
		case *ast.ResolveStmt:
			if _, ok := stmts[i].(*ast.ResolveStmt); !ok {
				t.Errorf("stmt[%d] = %T, want ResolveStmt", i, stmts[i])
			}
		case *ast.CommitStmt:
			if _, ok := stmts[i].(*ast.CommitStmt); !ok {
				t.Errorf("stmt[%d] = %T, want CommitStmt", i, stmts[i])
			}
		case *ast.EmitStmt:
			if _, ok := stmts[i].(*ast.EmitStmt); !ok {
				t.Errorf("stmt[%d] = %T, want EmitStmt", i, stmts[i])
			}
		case *ast.FormatStmt:
			if _, ok := stmts[i].(*ast.FormatStmt); !ok {
				t.Errorf("stmt[%d] = %T, want FormatStmt", i, stmts[i])
			}
		case *ast.RedirectStmt:
			if _, ok := stmts[i].(*ast.RedirectStmt); !ok {
				t.Errorf("stmt[%d] = %T, want RedirectStmt", i, stmts[i])
			}
		case *ast.LayoutStmt:
			if _, ok := stmts[i].(*ast.LayoutStmt); !ok {
				t.Errorf("stmt[%d] = %T, want LayoutStmt", i, stmts[i])
			}
		}
	}

	// Spot-check a few inner shapes.
	resolve := stmts[6].(*ast.ResolveStmt)
	if resolve.Name != "user" || resolve.Call == nil || resolve.Call.Name != "db.users" {
		t.Errorf("resolve shape wrong: %+v", resolve)
	}
	commitNamed := stmts[7].(*ast.CommitStmt)
	if commitNamed.Name != "user" {
		t.Errorf("commit named: name = %q, want user", commitNamed.Name)
	}
	commitBare := stmts[8].(*ast.CommitStmt)
	if commitBare.Name != "" {
		t.Errorf("commit bare: name = %q, want empty", commitBare.Name)
	}
	format := stmts[10].(*ast.FormatStmt)
	if format.Template != "user.show.html" || format.Layout != "admin" || len(format.Data) != 1 {
		t.Errorf("format shape wrong: %+v", format)
	}
	redirect := stmts[11].(*ast.RedirectStmt)
	if redirect.URL != "/users/:user.id" {
		t.Errorf("redirect URL = %q, want /users/:user.id", redirect.URL)
	}
}

func TestParseNoneStmt(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  approve none\n")
	mustNoErrors(t, errs)
	stmts := prog.System.Statements
	if len(stmts) != 1 {
		t.Fatalf("stmts = %d, want 1", len(stmts))
	}
	none, ok := stmts[0].(*ast.NoneStmt)
	if !ok {
		t.Fatalf("expected NoneStmt, got %T", stmts[0])
	}
	if none.Stage != "approve" {
		t.Errorf("stage = %q, want approve", none.Stage)
	}
}

func TestParseValueReferenceForms(t *testing.T) {
	prog, errs := parseStr(t, `system ->
  resolve a = db.a(:id)
  resolve b = db.b(:user.id, :user.team.id)
  resolve c = db.c(limit=10, status="active")
  resolve d = db.d(body CreateUser, query ListQuery)
`)
	mustNoErrors(t, errs)
	stmts := prog.System.Statements
	if len(stmts) != 4 {
		t.Fatalf("stmts = %d, want 4", len(stmts))
	}

	// :id → RouteParamRef
	a := stmts[0].(*ast.ResolveStmt).Call
	if len(a.Args) != 1 {
		t.Fatalf("a args = %d, want 1", len(a.Args))
	}
	if rp, ok := a.Args[0].(*ast.RouteParamRef); !ok || rp.Name != "id" {
		t.Errorf("a arg 0 = %#v, want RouteParamRef(id)", a.Args[0])
	}

	// :user.id, :user.team.id → FieldRef each
	b := stmts[1].(*ast.ResolveStmt).Call
	if len(b.Args) != 2 {
		t.Fatalf("b args = %d, want 2", len(b.Args))
	}
	for i, want := range [][]string{{"user", "id"}, {"user", "team", "id"}} {
		fr, ok := b.Args[i].(*ast.FieldRef)
		if !ok {
			t.Errorf("b arg %d type = %T, want FieldRef", i, b.Args[i])
			continue
		}
		if fr.Root != want[0] {
			t.Errorf("b arg %d root = %q, want %q", i, fr.Root, want[0])
		}
		if len(fr.Path) != len(want)-1 {
			t.Errorf("b arg %d path len = %d, want %d", i, len(fr.Path), len(want)-1)
		}
	}

	// limit=10 → NamedArg(IntLit), status="active" → NamedArg(StringLit)
	c := stmts[2].(*ast.ResolveStmt).Call
	if len(c.Args) != 2 {
		t.Fatalf("c args = %d, want 2", len(c.Args))
	}
	na0 := c.Args[0].(*ast.NamedArg)
	if na0.Name != "limit" {
		t.Errorf("c arg 0 name = %q, want limit", na0.Name)
	}
	if il, ok := na0.Value.(*ast.IntLit); !ok || il.Value != 10 {
		t.Errorf("c arg 0 value = %#v, want IntLit(10)", na0.Value)
	}
	na1 := c.Args[1].(*ast.NamedArg)
	if sl, ok := na1.Value.(*ast.StringLit); !ok || sl.Value != "active" {
		t.Errorf("c arg 1 value = %#v, want StringLit(active)", na1.Value)
	}

	// body / query
	d := stmts[3].(*ast.ResolveStmt).Call
	if len(d.Args) != 2 {
		t.Fatalf("d args = %d, want 2", len(d.Args))
	}
	if br, ok := d.Args[0].(*ast.BodyRef); !ok || br.TypeName != "CreateUser" {
		t.Errorf("d arg 0 = %#v, want BodyRef(CreateUser)", d.Args[0])
	}
	if qr, ok := d.Args[1].(*ast.QueryRef); !ok || qr.TypeName != "ListQuery" {
		t.Errorf("d arg 1 = %#v, want QueryRef(ListQuery)", d.Args[1])
	}
}

func TestParseEmptyArgList(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  limit rate.ip()\n")
	mustNoErrors(t, errs)
	limit := prog.System.Statements[0].(*ast.LimitStmt)
	if len(limit.Call.Args) != 0 {
		t.Errorf("args = %d, want 0", len(limit.Call.Args))
	}
}

func TestParseRateLiteral(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  limit rate.ip(60/min)\n")
	mustNoErrors(t, errs)
	call := prog.System.Statements[0].(*ast.LimitStmt).Call
	rate, ok := call.Args[0].(*ast.RateLit)
	if !ok {
		t.Fatalf("arg type = %T, want RateLit", call.Args[0])
	}
	if rate.Count != 60 || rate.Unit != "min" {
		t.Errorf("rate = %d/%s, want 60/min", rate.Count, rate.Unit)
	}
}

func TestParseApproveExpressionPrecedence(t *testing.T) {
	// NOT a AND b OR c → ((NOT a) AND b) OR c
	prog, errs := parseStr(t, "system ->\n  approve NOT a AND b OR c\n")
	mustNoErrors(t, errs)
	expr := prog.System.Statements[0].(*ast.ApproveStmt).Expr

	or, ok := expr.(*ast.ApproveOr)
	if !ok {
		t.Fatalf("top-level = %T, want ApproveOr", expr)
	}
	and, ok := or.Left.(*ast.ApproveAnd)
	if !ok {
		t.Fatalf("or.Left = %T, want ApproveAnd", or.Left)
	}
	if _, ok := and.Left.(*ast.ApproveNot); !ok {
		t.Fatalf("and.Left = %T, want ApproveNot", and.Left)
	}
	if _, ok := and.Right.(*ast.ApproveCall); !ok {
		t.Fatalf("and.Right = %T, want ApproveCall", and.Right)
	}
	if _, ok := or.Right.(*ast.ApproveCall); !ok {
		t.Fatalf("or.Right = %T, want ApproveCall", or.Right)
	}
}

func TestParseApproveParenthesesOverridePrecedence(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  approve a AND (b OR c)\n")
	mustNoErrors(t, errs)
	expr := prog.System.Statements[0].(*ast.ApproveStmt).Expr
	and, ok := expr.(*ast.ApproveAnd)
	if !ok {
		t.Fatalf("top-level = %T, want ApproveAnd", expr)
	}
	if _, ok := and.Right.(*ast.ApproveOr); !ok {
		t.Fatalf("and.Right = %T, want ApproveOr (paren-overridden)", and.Right)
	}
}

func TestParseRoutePatternForms(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		segCount int
	}{
		{"root", "/", 0},
		{"single literal", "/users", 1},
		{"literal and param", "/users/:id", 2},
		{"with wildcard", "/admin/*", 2},
		{"hyphenated literal", "/v1-2/things", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, errs := parseStr(t, "GET "+tc.pattern+" ->\n  log :id\n")
			mustNoErrors(t, errs)
			if len(prog.Handlers) != 1 {
				t.Fatalf("got %d handlers, want 1", len(prog.Handlers))
			}
			pat := prog.Handlers[0].Pattern
			if len(pat.Segments) != tc.segCount {
				t.Fatalf("segments = %d, want %d", len(pat.Segments), tc.segCount)
			}
		})
	}
}

func TestParseWildcardNotFinalIsError(t *testing.T) {
	_, errs := parseStr(t, "GET /users/*/posts ->\n  log :id\n")
	if len(errs) == 0 {
		t.Fatalf("expected error for non-final wildcard")
	}
}

func TestParseTrailingSlashIsError(t *testing.T) {
	_, errs := parseStr(t, "GET /users/ ->\n  log :id\n")
	if len(errs) == 0 {
		t.Fatalf("expected error for trailing slash")
	}
}

func TestParseEmptySegmentIsError(t *testing.T) {
	_, errs := parseStr(t, "GET //users ->\n  log :id\n")
	if len(errs) == 0 {
		t.Fatalf("expected error for empty segment")
	}
}

func TestParseIncludeStubReportsError(t *testing.T) {
	_, errs := parseStr(t, "include admin.writ\n")
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	if !contains(errs[0].Message, "include not yet implemented") {
		t.Fatalf("error message = %q", errs[0].Message)
	}
}

func TestParseAlwaysReturnsNonNilProgram(t *testing.T) {
	prog, errs := parseStr(t, "@@@ utterly broken\n")
	if prog == nil {
		t.Fatalf("Program is nil")
	}
	if len(errs) == 0 {
		t.Fatalf("expected errors for broken input")
	}
}

func TestParseMultipleErrorsInOnePass(t *testing.T) {
	src := `system ->
  log :id
get /lower ->
  log :id
foo /bar ->
  log :id
`
	_, errs := parseStr(t, src)
	if len(errs) < 2 {
		t.Fatalf("expected multiple errors, got %d: %v", len(errs), errs)
	}
}

func TestParseMultipleFormatLines(t *testing.T) {
	prog, errs := parseStr(t, `system ->
  format users.json with users
  format users.html with users
`)
	mustNoErrors(t, errs)
	stmts := prog.System.Statements
	if len(stmts) != 2 {
		t.Fatalf("stmts = %d, want 2", len(stmts))
	}
	for i, s := range stmts {
		if _, ok := s.(*ast.FormatStmt); !ok {
			t.Errorf("stmt[%d] = %T, want FormatStmt", i, s)
		}
	}
}

func TestParseDeterministic(t *testing.T) {
	src := `system ->
  log :id
GET /users/:id ->
  resolve user = db.users(:id)
  format user.show.html with user
`
	a, errsA := parseStr(t, src)
	b, errsB := parseStr(t, src)
	if len(errsA) != 0 || len(errsB) != 0 {
		t.Fatalf("unexpected errors: A=%v B=%v", errsA, errsB)
	}
	if (a.System == nil) != (b.System == nil) {
		t.Errorf("system presence differs")
	}
	if len(a.Handlers) != len(b.Handlers) {
		t.Errorf("handler count differs: %d vs %d", len(a.Handlers), len(b.Handlers))
	}
	if len(a.System.Statements) != len(b.System.Statements) {
		t.Errorf("system stmt count differs")
	}
}

func TestParseSpansReferenceOriginalSource(t *testing.T) {
	prog, errs := parseStr(t, "system ->\n  log :id\n")
	mustNoErrors(t, errs)
	stmt := prog.System.Statements[0].(*ast.LogStmt)
	span := stmt.Span()
	if span.Start.Source == nil || span.Start.Source.Path != "test.writ" {
		t.Errorf("span source path = %v, want test.writ", span.Start.Source)
	}
	if string(span.Text()) == "" {
		t.Errorf("span text empty; want non-empty")
	}
}

func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
