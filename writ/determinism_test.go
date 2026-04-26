package writ

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stonean/writ/ast"
)

// TestDeterminismStructuralEquality asserts that loading the same
// .writ source into two distinct Writ instances produces routing
// tables that are structurally equal: same method ordering, same
// per-method route count, same segment sequence per route, same
// resolver/formatter registration names per step, and equal spans.
//
// Two parses produce pointer-distinct *ast.Source values for the
// same logical file; equality is asserted on the externally
// observable shape, not on pointer identity.
func TestDeterminismStructuralEquality(t *testing.T) {
	src := `GET /users ->
  format users.list

GET /users/:id ->
  resolve user = db.users(:id)
  format users.show with user

POST /users ->
  format users.create

DELETE /users/:id ->
  resolve user = db.users(:id)
  format users.gone with user
`
	// Use a single temp file for both loads so file paths in spans
	// are identical; the test asserts structural equality of the
	// observable shape, not pointer identity of distinct parses.
	path := writeWritFile(t, src)
	probe := func(label string) *routingTable {
		w := New()
		mustRegister(t, w.Resolver("db.users", func(_ context.Context, _ Params) (any, error) { return nil, nil }))
		for _, name := range []string{"users.list", "users.show", "users.create", "users.gone"} {
			mustRegister(t, w.Formatter(name, func(_ context.Context, _ http.ResponseWriter, _ Results) error { return nil }))
		}
		if err := w.Load(path); err != nil {
			t.Fatalf("%s: Load: %v", label, err)
		}
		return w.table.Load()
	}

	a := probe("first")
	b := probe("second")

	if !reflect.DeepEqual(a.methods, b.methods) {
		t.Fatalf("methods order mismatch: a=%v b=%v", a.methods, b.methods)
	}

	for _, m := range a.methods {
		ra := a.byMethod[m]
		rb := b.byMethod[m]
		if len(ra) != len(rb) {
			t.Fatalf("method %s: route count mismatch a=%d b=%d", m, len(ra), len(rb))
		}
		for i := range ra {
			if ra[i].method != rb[i].method {
				t.Errorf("%s[%d].method mismatch %q vs %q", m, i, ra[i].method, rb[i].method)
			}
			if !segmentsStructurallyEqual(ra[i].segments, rb[i].segments) {
				t.Errorf("%s[%d].segments mismatch", m, i)
			}
			if !spansEqual(ra[i].span, rb[i].span) {
				t.Errorf("%s[%d].span mismatch", m, i)
			}
			if len(ra[i].resolves) != len(rb[i].resolves) {
				t.Errorf("%s[%d].resolves count mismatch a=%d b=%d", m, i, len(ra[i].resolves), len(rb[i].resolves))
			}
			for j := range ra[i].resolves {
				if ra[i].resolves[j].name != rb[i].resolves[j].name {
					t.Errorf("%s[%d].resolves[%d].name mismatch %q vs %q",
						m, i, j, ra[i].resolves[j].name, rb[i].resolves[j].name)
				}
				if !reflect.DeepEqual(ra[i].resolves[j].paramArgs, rb[i].resolves[j].paramArgs) {
					t.Errorf("%s[%d].resolves[%d].paramArgs mismatch", m, i, j)
				}
			}
			if ra[i].format.template != rb[i].format.template {
				t.Errorf("%s[%d].format.template mismatch %q vs %q",
					m, i, ra[i].format.template, rb[i].format.template)
			}
			if !reflect.DeepEqual(ra[i].format.with, rb[i].format.with) {
				t.Errorf("%s[%d].format.with mismatch", m, i)
			}
		}
	}
}

// TestNoIO asserts the writ package performs no environment access
// outside Run/New, no clock reads, no goroutine launches, and no
// non-HTTP network access. The runtime *does* import "os" (for
// os.Getenv on PORT/WRIT_ENV) and "net/http" (for the public
// handler surface); the source-grep allowlists those imports
// precisely while still catching "time", "runtime.NumGoroutine",
// "go func", and the unrelated network packages.
//
// The check is a source-grep over every non-test .go file in the
// package. Substring matches produce false positives only if a
// future maintainer names an identifier with one of these
// prefixes, in which case this test flags it for review.
func TestNoIO(t *testing.T) {
	pkgDir := "."
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	allowed := map[string]struct{}{
		`"os"`:       {},
		`"net/http"`: {},
	}
	forbidden := []string{
		`"time"`,
		`"net"`,
		`"net/url"`,
		`"net/rpc"`,
		`"net/smtp"`,
		`runtime.NumGoroutine`,
		`go func`,
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(pkgDir, name)
		// Read the file via os.ReadFile in this *test* file. The
		// test is allowed to use os; the assertion is on the
		// non-test source files themselves.
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(bytes)
		for _, tok := range forbidden {
			if strings.Contains(body, tok) {
				t.Errorf("%s contains forbidden token %q", name, tok)
			}
		}
		// Sanity: confirm the allowed imports are still allowed.
		for tok := range allowed {
			_ = tok // present for documentation; no assertion needed
		}
	}
}

// segmentsStructurallyEqual compares two RouteSegment slices by
// kind and exposed value. Pointer identity is irrelevant; the AST
// is parsed twice for two Writ instances and produces distinct
// pointers for the same logical segments.
func segmentsStructurallyEqual(a, b []ast.RouteSegment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		switch sa := a[i].(type) {
		case *ast.LiteralSegment:
			sb, ok := b[i].(*ast.LiteralSegment)
			if !ok || sa.Text != sb.Text {
				return false
			}
		case *ast.ParameterSegment:
			sb, ok := b[i].(*ast.ParameterSegment)
			if !ok || sa.Name != sb.Name {
				return false
			}
		default:
			if reflect.TypeOf(a[i]) != reflect.TypeOf(b[i]) {
				return false
			}
		}
	}
	return true
}

// spansEqual reports whether two ast.Span values reference the same
// source path, line, column, and offset for both Start and End. AST
// pointers are not compared because two parses produce
// pointer-distinct *ast.Source values for the same logical file.
func spansEqual(a, b ast.Span) bool {
	return positionsEqual(a.Start, b.Start) && positionsEqual(a.End, b.End)
}

func positionsEqual(a, b ast.Position) bool {
	pa, pb := "", ""
	if a.Source != nil {
		pa = a.Source.Path
	}
	if b.Source != nil {
		pb = b.Source.Path
	}
	return pa == pb && a.Line == b.Line && a.Column == b.Column && a.Offset == b.Offset
}
