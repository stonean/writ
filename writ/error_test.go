package writ

import (
	"errors"
	"strings"
	"testing"

	"github.com/stonean/writ/ast"
)

func TestErrorKindString(t *testing.T) {
	cases := map[ErrorKind]string{
		KindParseFailure:             "parse-failure",
		KindElaborationFailure:       "elaboration-failure",
		KindUnregisteredResolver:     "unregistered-resolver",
		KindUnregisteredFormatter:    "unregistered-formatter",
		KindUnsupportedStage:         "unsupported-stage",
		KindUndeclaredRouteParameter: "undeclared-route-parameter",
		KindRouteAmbiguity:           "route-ambiguity",
		KindMissingEnvVar:            "missing-env-var",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("(%d).String() = %q, want %q", int(kind), got, want)
		}
	}
}

func TestErrorKindStringUnknown(t *testing.T) {
	got := ErrorKind(999).String()
	if !strings.Contains(got, "999") {
		t.Errorf("unknown kind String() = %q, want to include the constant value", got)
	}
}

func TestEntryErrorWithSpan(t *testing.T) {
	src := &ast.Source{Path: "p.writ"}
	pos := ast.Position{Source: src, Line: 4, Column: 7}
	entry := Entry{
		Kind:    KindUnregisteredResolver,
		Message: `resolver "db.users" is not registered`,
		Span:    ast.Span{Start: pos, End: pos},
	}
	want := `p.writ:4:7: unregistered-resolver: resolver "db.users" is not registered`
	if got := entry.Error(); got != want {
		t.Errorf("Entry.Error() =\n  %q\nwant\n  %q", got, want)
	}
}

func TestEntryErrorWithoutSpan(t *testing.T) {
	entry := Entry{
		Kind:    KindMissingEnvVar,
		Message: "environment variable PORT is required and not set",
	}
	want := "missing-env-var: environment variable PORT is required and not set"
	if got := entry.Error(); got != want {
		t.Errorf("Entry.Error() =\n  %q\nwant\n  %q", got, want)
	}
}

func TestErrorEmptyAggregate(t *testing.T) {
	e := &Error{}
	if got := e.Error(); got != "" {
		t.Errorf("empty Error.Error() = %q, want empty", got)
	}
	if got := e.Unwrap(); got != nil {
		t.Errorf("empty Error.Unwrap() = %v, want nil", got)
	}
}

func TestErrorNilReceiver(t *testing.T) {
	var e *Error
	if got := e.Error(); got != "" {
		t.Errorf("nil Error.Error() = %q, want empty", got)
	}
	if got := e.Unwrap(); got != nil {
		t.Errorf("nil Error.Unwrap() = %v, want nil", got)
	}
}

func TestErrorMultipleEntriesFormatJoinedByNewlines(t *testing.T) {
	src := &ast.Source{Path: "p.writ"}
	span := ast.Span{
		Start: ast.Position{Source: src, Line: 1, Column: 1},
		End:   ast.Position{Source: src, Line: 1, Column: 1},
	}
	e := &Error{
		Entries: []Entry{
			{Kind: KindUnregisteredResolver, Message: "first", Span: span},
			{Kind: KindUnregisteredFormatter, Message: "second", Span: span},
		},
	}
	got := e.Error()
	if !strings.Contains(got, "unregistered-resolver: first") {
		t.Errorf("missing first entry in %q", got)
	}
	if !strings.Contains(got, "unregistered-formatter: second") {
		t.Errorf("missing second entry in %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("entries should be newline-joined; got %q", got)
	}
}

func TestErrorAsTargetsAggregatePointer(t *testing.T) {
	src := &ast.Source{Path: "p.writ"}
	span := ast.Span{
		Start: ast.Position{Source: src, Line: 1, Column: 1},
		End:   ast.Position{Source: src, Line: 1, Column: 1},
	}
	e := &Error{Entries: []Entry{{Kind: KindUnregisteredResolver, Message: "x", Span: span}}}

	var target *Error
	if !errors.As(e, &target) {
		t.Fatalf("errors.As(*Error) returned false")
	}
	if target == nil || len(target.Entries) != 1 {
		t.Errorf("target = %+v, want one entry", target)
	}
}

func TestErrorUnwrapReturnsPerEntryErrors(t *testing.T) {
	src := &ast.Source{Path: "p.writ"}
	span := ast.Span{
		Start: ast.Position{Source: src, Line: 1, Column: 1},
		End:   ast.Position{Source: src, Line: 1, Column: 1},
	}
	e := &Error{
		Entries: []Entry{
			{Kind: KindUnregisteredResolver, Message: "first", Span: span},
			{Kind: KindUnregisteredFormatter, Message: "second", Span: span},
		},
	}
	unwrapped := e.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("Unwrap returned %d errors, want 2", len(unwrapped))
	}
	for i, want := range []string{"unregistered-resolver", "unregistered-formatter"} {
		if !strings.Contains(unwrapped[i].Error(), want) {
			t.Errorf("unwrapped[%d] = %q, missing %q", i, unwrapped[i].Error(), want)
		}
	}
}

// Entry contains a []ast.Span field, so it is not directly
// comparable with `==` and therefore not usable as a target for
// errors.Is. Consumers that want to switch on kind read the entry
// list via errors.As(*Error) and inspect Entries[i].Kind directly.
// This test pins that contract: Unwrap is the seam that makes the
// inner error available to errors.As, but Is matching against an
// Entry value is not a supported pattern.
func TestErrorAsExposesEntryListForKindSwitching(t *testing.T) {
	src := &ast.Source{Path: "p.writ"}
	span := ast.Span{
		Start: ast.Position{Source: src, Line: 1, Column: 1},
		End:   ast.Position{Source: src, Line: 1, Column: 1},
	}
	e := &Error{Entries: []Entry{
		{Kind: KindUnregisteredResolver, Message: "x", Span: span},
		{Kind: KindRouteAmbiguity, Message: "y", Span: span},
	}}

	var target *Error
	if !errors.As(e, &target) {
		t.Fatalf("errors.As(*Error) returned false")
	}
	gotKinds := []ErrorKind{}
	for _, entry := range target.Entries {
		gotKinds = append(gotKinds, entry.Kind)
	}
	want := []ErrorKind{KindUnregisteredResolver, KindRouteAmbiguity}
	if len(gotKinds) != len(want) || gotKinds[0] != want[0] || gotKinds[1] != want[1] {
		t.Errorf("kinds = %v, want %v", gotKinds, want)
	}
}
