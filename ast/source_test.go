package ast

import (
	"testing"
)

func TestPositionOrdering(t *testing.T) {
	src := &Source{Path: "test.writ", Bytes: []byte("hello\nworld\n")}

	cases := []struct {
		name string
		a, b Position
		less bool
	}{
		{
			name: "earlier line is less",
			a:    Position{Source: src, Line: 1, Column: 5, Offset: 4},
			b:    Position{Source: src, Line: 2, Column: 1, Offset: 6},
			less: true,
		},
		{
			name: "earlier column on same line is less",
			a:    Position{Source: src, Line: 1, Column: 1, Offset: 0},
			b:    Position{Source: src, Line: 1, Column: 4, Offset: 3},
			less: true,
		},
		{
			name: "equal positions are not less",
			a:    Position{Source: src, Line: 1, Column: 1, Offset: 0},
			b:    Position{Source: src, Line: 1, Column: 1, Offset: 0},
			less: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.Offset < tc.b.Offset
			if got != tc.less {
				t.Fatalf("offset ordering: got %v, want %v", got, tc.less)
			}
			if tc.a == tc.b && tc.less {
				t.Fatalf("equal positions reported as less")
			}
		})
	}
}

func TestSpanText(t *testing.T) {
	src := &Source{Path: "test.writ", Bytes: []byte("system ->\n  log :id\n")}

	cases := []struct {
		name string
		span Span
		want string
	}{
		{
			name: "first token",
			span: Span{
				Start: Position{Source: src, Line: 1, Column: 1, Offset: 0},
				End:   Position{Source: src, Line: 1, Column: 7, Offset: 6},
			},
			want: "system",
		},
		{
			name: "second-line statement",
			span: Span{
				Start: Position{Source: src, Line: 2, Column: 3, Offset: 12},
				End:   Position{Source: src, Line: 2, Column: 10, Offset: 19},
			},
			want: "log :id",
		},
		{
			name: "empty span yields empty bytes",
			span: Span{
				Start: Position{Source: src, Line: 1, Column: 1, Offset: 0},
				End:   Position{Source: src, Line: 1, Column: 1, Offset: 0},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(tc.span.Text())
			if got != tc.want {
				t.Fatalf("Span.Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpanTextZeroValueReturnsNil(t *testing.T) {
	var s Span
	if got := s.Text(); got != nil {
		t.Fatalf("zero Span.Text() = %q, want nil", got)
	}
}

func TestSpanTextDoesNotMutate(t *testing.T) {
	src := &Source{Path: "test.writ", Bytes: []byte("abc")}
	span := Span{
		Start: Position{Source: src, Line: 1, Column: 1, Offset: 0},
		End:   Position{Source: src, Line: 1, Column: 4, Offset: 3},
	}
	_ = span.Text()
	if string(src.Bytes) != "abc" {
		t.Fatalf("Source.Bytes mutated: got %q", src.Bytes)
	}
}
