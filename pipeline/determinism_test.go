package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stonean/writ/ast"
	"github.com/stonean/writ/parser"
)

// TestDeterminismStructuralEquality asserts that elaborating
// structurally-equivalent input twice produces structurally-equal
// *Resolved values: same handler count, per-handler Stages kind +
// span + source-level sequence, OptOuts kind + span sequence, and
// ErrorMap entry sequence.
//
// The two parses produce pointer-distinct AST nodes; equality is
// asserted on the externally observable shape, not on pointer
// identity.
func TestDeterminismStructuralEquality(t *testing.T) {
	src := `system ->
  approve auth.isUser
  resolve current_user = auth.user()

group /admin/* ->
  approve auth.isAdmin
  resolve sysstats = db.stats()

group /admin/users/* ->
  resolve user = db.users(:id)

errors /* ->
  ServerError serverErrJSON
  default defaultJSON

errors /admin/* ->
  NotFound notFoundJSON

GET /admin/users/:id ->
  layout none
  resolve perms = auth.perms(:id)
  format u.html with user, perms

GET /home ->
  format home.html with current_user
`
	probe := func(label string) (*Resolved, []Error) {
		prog, perrs := parser.ParseString("p.writ", src)
		if len(perrs) != 0 {
			t.Fatalf("%s: parse errors: %v", label, perrs)
		}
		return Elaborate(prog)
	}

	a, errsA := probe("first")
	b, errsB := probe("second")

	if len(errsA) != len(errsB) {
		t.Fatalf("error count mismatch: a=%d b=%d", len(errsA), len(errsB))
	}
	for i := range errsA {
		if errsA[i].Kind != errsB[i].Kind ||
			errsA[i].Message != errsB[i].Message ||
			!spansEqual(errsA[i].Span, errsB[i].Span) {
			t.Errorf("error[%d] mismatch:\n a=%+v\n b=%+v", i, errsA[i], errsB[i])
		}
	}

	if len(a.Handlers) != len(b.Handlers) {
		t.Fatalf("handler count mismatch: a=%d b=%d", len(a.Handlers), len(b.Handlers))
	}
	for i, ha := range a.Handlers {
		hb := b.Handlers[i]
		if ha.Method != hb.Method {
			t.Errorf("handler[%d].Method mismatch: %q vs %q", i, ha.Method, hb.Method)
		}

		if len(ha.Stages) != len(hb.Stages) {
			t.Errorf("handler[%d].Stages length mismatch: a=%d b=%d",
				i, len(ha.Stages), len(hb.Stages))
			continue
		}
		for j, sa := range ha.Stages {
			sb := hb.Stages[j]
			if sa.Kind() != sb.Kind() {
				t.Errorf("handler[%d].Stages[%d].Kind mismatch: %v vs %v",
					i, j, sa.Kind(), sb.Kind())
			}
			if sa.SourceLevel() != sb.SourceLevel() {
				t.Errorf("handler[%d].Stages[%d].SourceLevel mismatch: %v vs %v",
					i, j, sa.SourceLevel(), sb.SourceLevel())
			}
			if !spansEqual(sa.Span(), sb.Span()) {
				t.Errorf("handler[%d].Stages[%d].Span mismatch", i, j)
			}
		}

		if len(ha.OptOuts) != len(hb.OptOuts) {
			t.Errorf("handler[%d].OptOuts length mismatch: a=%d b=%d",
				i, len(ha.OptOuts), len(hb.OptOuts))
			continue
		}
		for j, oa := range ha.OptOuts {
			ob := hb.OptOuts[j]
			if oa.Kind != ob.Kind {
				t.Errorf("handler[%d].OptOuts[%d].Kind mismatch: %v vs %v",
					i, j, oa.Kind, ob.Kind)
			}
			if !spansEqual(oa.Span, ob.Span) {
				t.Errorf("handler[%d].OptOuts[%d].Span mismatch", i, j)
			}
		}

		if len(ha.ErrorMap) != len(hb.ErrorMap) {
			t.Errorf("handler[%d].ErrorMap length mismatch: a=%d b=%d",
				i, len(ha.ErrorMap), len(hb.ErrorMap))
			continue
		}
		for j, ea := range ha.ErrorMap {
			eb := hb.ErrorMap[j]
			if ea.TypeName != eb.TypeName {
				t.Errorf("handler[%d].ErrorMap[%d].TypeName mismatch: %q vs %q",
					i, j, ea.TypeName, eb.TypeName)
			}
			if ea.Formatter != eb.Formatter {
				t.Errorf("handler[%d].ErrorMap[%d].Formatter mismatch: %q vs %q",
					i, j, ea.Formatter, eb.Formatter)
			}
			if !spansEqual(ea.TypeSpan, eb.TypeSpan) {
				t.Errorf("handler[%d].ErrorMap[%d].TypeSpan mismatch", i, j)
			}
		}
	}
}

// TestNoIO asserts the pipeline package performs no I/O, no
// environment access, no clock reads, no goroutine launches, and no
// network access.
//
// The check is a source-grep over every non-test .go file in the
// package. The forbidden tokens are conservative — substring matches
// produce false positives only if a future maintainer names an
// identifier with one of these prefixes, in which case the test will
// flag it for review. The assertion is the contract; the elaborator's
// determinism rests on it.
func TestNoIO(t *testing.T) {
	pkgDir := "."
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	forbidden := []string{
		`"os"`,
		`"time"`,
		`"net"`,
		`"net/http"`,
		`"net/url"`,
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
				t.Errorf("%s contains forbidden token %q (pipeline must perform no I/O)",
					name, tok)
			}
		}
	}
}

// spansEqual reports whether two ast.Span values reference the same
// source path, line, column, and offset for both Start and End.
// AST pointers are not compared because two parses produce
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
