package parser

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// These tests fill gaps in coverage that the acceptance suite does not
// exercise — primarily error paths inside the parser plus a handful of
// surface-area features (negative integer literals, dotted with-clause
// references, hyphenated parameter names, the WithRoot option) that
// the existing tests touch only indirectly.

func TestParseNonExistentFile(t *testing.T) {
	prog, errs := Parse("does/not/exist.writ", WithFS(fstest.MapFS{}))
	if prog == nil {
		t.Fatal("Parse returned nil program; should always be non-nil")
	}
	if len(errs) == 0 {
		t.Fatal("expected at least one error for missing root file")
	}
}

func TestWithRootOption(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte("system ->\n  approve auth.x\n")},
	}
	prog, errs := Parse("app.writ", WithFS(fsys), WithRoot("."))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if prog.System == nil {
		t.Fatal("expected a system block")
	}
}

func TestParseLiteralNegativeInteger(t *testing.T) {
	src := `system ->
  limit rate.ip(quota=-5)
`
	prog, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if prog.System == nil || len(prog.System.Statements) != 1 {
		t.Fatalf("expected one system statement, got %v", prog.System)
	}
}

func TestParseLiteralRate(t *testing.T) {
	src := `system ->
  limit rate.ip(60/min)
`
	prog, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	_ = prog
}

func TestParseHyphenatedParameterSegment(t *testing.T) {
	src := `GET /users/:user-id/profile ->
  format user.show.json
`
	_, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors for hyphenated param: %v", errs)
	}
}

func TestParseDottedNamedRef(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show.json with user.profile, user.email
`
	_, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestParseLiteralUnexpectedToken(t *testing.T) {
	// `limit rate.ip(quota=)` — equals followed by ')' is no literal.
	src := `system ->
  limit rate.ip(quota=)
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected an error for missing literal after '='")
	}
}

func TestParseSessionStmtMissingStorage(t *testing.T) {
	src := `system ->
  session
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for session with no storage")
	}
}

func TestParseLayoutStmtMissingName(t *testing.T) {
	src := `system ->
  layout
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for layout with no name")
	}
}

func TestParseRedirectStmtMissingURL(t *testing.T) {
	src := `GET /old ->
  redirect
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for redirect with no URL")
	}
}

func TestParseFormatStmtMissingTemplate(t *testing.T) {
	src := `GET /x ->
  format
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for format with no template")
	}
}

func TestParseResolveStmtMissingEquals(t *testing.T) {
	src := `system ->
  resolve user db.users()
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for resolve without '='")
	}
}

func TestParseEmitStmtMissingEvent(t *testing.T) {
	src := `system ->
  emit
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for emit with no event name")
	}
}

func TestParseApproveExprWithNot(t *testing.T) {
	src := `GET /admin ->
  approve NOT auth.banned() AND auth.staff()
  format admin.json
`
	_, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestParseApproveExprParenthesized(t *testing.T) {
	src := `GET /x ->
  approve (auth.a() OR auth.b()) AND auth.c()
  format x.json
`
	_, errs := ParseString("p.writ", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestParseGroupBlockMissingArrow(t *testing.T) {
	src := `group /admin/*
  approve auth.staff()
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for group missing '->'")
	}
}

func TestParseErrorsBlockEntryMissingFormatter(t *testing.T) {
	src := `errors /* ->
  NotFound
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for errors entry missing formatter")
	}
}

func TestParseStatementUnknownKeyword(t *testing.T) {
	src := `system ->
  bogus arg
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown statement keyword")
	}
}

func TestParseHandlerLowercaseMethodRejected(t *testing.T) {
	src := `get /x ->
  format x.json
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for lowercase method")
	}
}

func TestParseRouteWildcardNotFinalRejected(t *testing.T) {
	src := `GET /x/*/y ->
  format x.json
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for non-final wildcard")
	}
}

func TestParseStringLiteralUnterminated(t *testing.T) {
	src := `system ->
  limit rate.ip(name="oops)
`
	_, errs := ParseString("p.writ", src)
	if len(errs) == 0 {
		t.Fatal("expected error for unterminated string")
	}
}

func TestParseIncludeMissingFile(t *testing.T) {
	src := `include "missing.writ"
`
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(src)},
	}
	_, errs := Parse("app.writ", WithFS(fsys))
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "missing.writ") || strings.Contains(strings.ToLower(e.Message), "include") || strings.Contains(strings.ToLower(e.Message), "open") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an include-related error, got %v", errs)
	}
}

func TestErrorTypeImplementsErrorInterface(t *testing.T) {
	var e error = Error{File: "x.writ", Line: 1, Column: 2, Message: "boom"}
	if !strings.Contains(e.Error(), "boom") {
		t.Errorf("Error() = %q, want it to contain 'boom'", e.Error())
	}
}

func TestParseDirectFileViaFS(t *testing.T) {
	// Exercises Parse's fs.ReadFile branch with a valid file.
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte("system ->\n  approve auth.x()\n")},
	}
	prog, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if prog.System == nil {
		t.Fatal("expected system block")
	}
}

func TestParseFSReadError(t *testing.T) {
	// fs.ReadFile fails for a missing file; ensure Parse handles it.
	fsys := fstest.MapFS{}
	prog, errs := Parse("missing.writ", WithFS(fsys))
	if prog == nil {
		t.Fatal("Parse returned nil")
	}
	if len(errs) == 0 {
		t.Fatal("expected an error for missing file")
	}
	// The error message should reference the file.
	if !strings.Contains(errs[0].Message, "missing.writ") {
		t.Errorf("error message should reference filename, got %q", errs[0].Message)
	}
}

// statSentinel and openErrFS exist so we can also exercise fs error
// pathways beyond plain "not found" — for example a directory-instead-
// of-file scenario.
var _ fs.FS = errFS{}

type errFS struct{}

func (errFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
}

func TestParseFSCustomError(t *testing.T) {
	prog, errs := Parse("anything.writ", WithFS(errFS{}))
	if prog == nil || len(errs) == 0 {
		t.Fatal("expected program + errors when fs always errors")
	}
}
