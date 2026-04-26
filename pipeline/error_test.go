package pipeline

import (
	"testing"

	"github.com/stonean/writ/ast"
)

func TestErrorFormat(t *testing.T) {
	src := &ast.Source{Path: "app.writ", Bytes: []byte("system ->\n  format x\n")}
	span := ast.Span{
		Start: ast.Position{Source: src, Line: 2, Column: 3, Offset: 12},
		End:   ast.Position{Source: src, Line: 2, Column: 11, Offset: 20},
	}
	e := newError(StagePlacement, span, "format not allowed in system block")

	got := e.Error()
	want := "app.writ:2:3: format not allowed in system block"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if e.Kind != StagePlacement {
		t.Fatalf("Kind = %v, want StagePlacement", e.Kind)
	}
	if e.Span != span {
		t.Fatalf("Span mismatch")
	}
	if len(e.Spans) != 0 {
		t.Fatalf("Spans should be empty for single-site error, got %d", len(e.Spans))
	}
}

func TestErrorKindString(t *testing.T) {
	cases := []struct {
		kind ErrorKind
		want string
	}{
		{StagePlacement, "stage-placement"},
		{StageOrder, "stage-order"},
		{AmbiguousGroup, "ambiguous-group"},
		{AmbiguousErrorsBlock, "ambiguous-errors-block"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("ErrorKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}
