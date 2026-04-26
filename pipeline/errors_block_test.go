package pipeline

import (
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
)

// makeHandler builds a HandlerBlock with the given route pattern and a
// span anchored to a synthetic source line.
func makeHandler(t *testing.T, route string, line int) *ast.HandlerBlock {
	t.Helper()
	src := newSrc("h.writ")
	h := ast.NewHandlerBlock(span(src, line))
	h.Method = "GET"
	h.Pattern = pat(t, route)
	return h
}

// makeErrorsBlock builds an ErrorsBlock with the given route pattern,
// declared on a specific source line, plus a list of (typeName,
// formatter) entries.
func makeErrorsBlock(t *testing.T, route string, line int, entries [][2]string) *ast.ErrorsBlock {
	t.Helper()
	src := newSrc("e.writ")
	b := ast.NewErrorsBlock(span(src, line))
	b.Pattern = pat(t, route)
	for i, ent := range entries {
		entrySpan := span(src, line+i+1)
		e := ast.NewErrorsEntry(entrySpan)
		e.TypeName = ent[0]
		e.TypeSpan = entrySpan
		e.Formatter = ent[1]
		e.FormatterSpan = entrySpan
		e.IsDefault = ent[0] == "default"
		b.Entries = append(b.Entries, e)
	}
	return b
}

func entryTypes(entries []ErrorMapEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.TypeName
	}
	return out
}

func entryFormatters(entries []ErrorMapEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Formatter
	}
	return out
}

// --- Errors Block Selection ---

func TestBuildErrorMapNoMatchingBlocks(t *testing.T) {
	h := makeHandler(t, "/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/admin/*", 5, [][2]string{{"NotFound", "x"}}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", entryTypes(entries))
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
}

func TestBuildErrorMapSingleMatchingBlock(t *testing.T) {
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/admin/*", 5, [][2]string{
			{"NotFound", "not_found.json"},
			{"Validation", "validation.json"},
		}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
	if !reflect.DeepEqual(entryTypes(entries), []string{"NotFound", "Validation"}) {
		t.Fatalf("types = %v, want [NotFound Validation]", entryTypes(entries))
	}
}

func TestBuildErrorMapTwoBlocksLayerInContainment(t *testing.T) {
	// /admin/* is more specific than /*
	// /admin/* declares DuplicateEmail; /* declares NotFound and default.
	// Effective for handler /admin/users/1: DuplicateEmail (from /admin/*),
	// NotFound and default (from /*).
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/*", 5, [][2]string{
			{"NotFound", "not_found.json"},
			{"default", "error.json"},
		}),
		makeErrorsBlock(t, "/admin/*", 10, [][2]string{
			{"DuplicateEmail", "conflict.json"},
		}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
	want := []string{"DuplicateEmail", "NotFound", "default"}
	if !reflect.DeepEqual(entryTypes(entries), want) {
		t.Fatalf("types = %v, want %v", entryTypes(entries), want)
	}
}

func TestBuildErrorMapMostSpecificWinsForSameType(t *testing.T) {
	// Both blocks declare NotFound; /admin/*'s wins.
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/*", 5, [][2]string{
			{"NotFound", "general_not_found.json"},
		}),
		makeErrorsBlock(t, "/admin/*", 10, [][2]string{
			{"NotFound", "admin_not_found.json"},
		}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Formatter != "admin_not_found.json" {
		t.Errorf("Formatter = %q, want admin_not_found.json (more specific)", entries[0].Formatter)
	}
	if entries[0].SourceBlock.Span().Start.Line != 10 {
		t.Errorf("SourceBlock line = %d, want 10", entries[0].SourceBlock.Span().Start.Line)
	}
}

func TestBuildErrorMapEmptyEntriesPreserveSourceSpans(t *testing.T) {
	h := makeHandler(t, "/admin/x", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/admin/*", 5, [][2]string{
			{"NotFound", "not_found.json"},
		}),
	}
	entries, _ := buildErrorMap(h, blocks)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].TypeSpan.Start.Line == 0 {
		t.Errorf("TypeSpan should be set, got zero span")
	}
	if entries[0].FormatterSpan.Start.Line == 0 {
		t.Errorf("FormatterSpan should be set, got zero span")
	}
	if entries[0].SourceBlock == nil {
		t.Errorf("SourceBlock should be non-nil")
	}
}

func TestBuildErrorMapEmptyMapWhenNoBlocks(t *testing.T) {
	h := makeHandler(t, "/x", 1)
	entries, errs := buildErrorMap(h, nil)
	if len(entries) != 0 || len(errs) != 0 {
		t.Fatalf("entries=%v errs=%v, want both empty", entries, errs)
	}
}

func TestBuildErrorMapNilHandlerSafe(t *testing.T) {
	entries, errs := buildErrorMap(nil, nil)
	if len(entries) != 0 || len(errs) != 0 {
		t.Fatalf("entries=%v errs=%v, want both empty", entries, errs)
	}
}

// --- Ambiguous Errors Block Membership ---

func TestBuildErrorMapAmbiguityReportsAllConflictingBlocks(t *testing.T) {
	// /admin/* and /:tenant/users/* both match /admin/users/1
	// without one containing the other.
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/admin/*", 5, [][2]string{{"X", "x.json"}}),
		makeErrorsBlock(t, "/:tenant/users/*", 10, [][2]string{{"Y", "y.json"}}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 1 {
		t.Fatalf("expected 1 ambiguity error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != AmbiguousErrorsBlock {
		t.Errorf("Kind = %v, want AmbiguousErrorsBlock", errs[0].Kind)
	}
	if errs[0].Span.Start.Line != 1 {
		t.Errorf("primary span = line %d, want 1 (handler)", errs[0].Span.Start.Line)
	}
	if len(errs[0].Spans) != 2 {
		t.Errorf("Spans len = %d, want 2 (both conflicting blocks)", len(errs[0].Spans))
	}
	// Both blocks are conflicting; effective map is empty.
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty after full ambiguity", entryTypes(entries))
	}
}

func TestBuildErrorMapAmbiguityWithCleanChainKept(t *testing.T) {
	// /* and /admin/* form a chain; /:tenant/users/* is the outlier.
	// Only /:tenant/users/* should be reported as conflicting; the
	// chain layers normally.
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/*", 5, [][2]string{
			{"NotFound", "not_found.json"},
		}),
		makeErrorsBlock(t, "/admin/*", 10, [][2]string{
			{"DuplicateEmail", "conflict.json"},
		}),
		makeErrorsBlock(t, "/:tenant/users/*", 15, [][2]string{
			{"Skipped", "skipped.json"},
		}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 1 {
		t.Fatalf("expected 1 ambiguity error, got %d", len(errs))
	}
	if len(errs[0].Spans) != 1 {
		t.Errorf("Spans len = %d, want 1 (only the outlier block)", len(errs[0].Spans))
	}
	want := []string{"DuplicateEmail", "NotFound"}
	if !reflect.DeepEqual(entryTypes(entries), want) {
		t.Fatalf("types = %v, want %v", entryTypes(entries), want)
	}
	for _, e := range entries {
		if e.TypeName == "Skipped" {
			t.Fatalf("conflicting block's entry leaked into effective map")
		}
	}
}

func TestBuildErrorMapAmbiguityNoCleanChainEmptyMap(t *testing.T) {
	// Two overlapping blocks, no chain partner for either.
	// Effective map is empty.
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/admin/*", 5, [][2]string{{"NotFound", "x.json"}}),
		makeErrorsBlock(t, "/:tenant/users/*", 10, [][2]string{{"Validation", "y.json"}}),
	}
	entries, errs := buildErrorMap(h, blocks)
	if len(errs) != 1 {
		t.Fatalf("expected 1 ambiguity error, got %d", len(errs))
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty (no clean chain)", entryTypes(entries))
	}
}

// --- Determinism ---

func TestBuildErrorMapDeterministic(t *testing.T) {
	h := makeHandler(t, "/admin/users/1", 1)
	blocks := []*ast.ErrorsBlock{
		makeErrorsBlock(t, "/*", 5, [][2]string{
			{"NotFound", "a.json"},
			{"default", "b.json"},
		}),
		makeErrorsBlock(t, "/admin/*", 10, [][2]string{
			{"DuplicateEmail", "c.json"},
		}),
	}
	a, ae := buildErrorMap(h, blocks)
	b, be := buildErrorMap(h, blocks)
	if !reflect.DeepEqual(entryTypes(a), entryTypes(b)) {
		t.Fatalf("types differ between runs: %v vs %v", entryTypes(a), entryTypes(b))
	}
	if !reflect.DeepEqual(entryFormatters(a), entryFormatters(b)) {
		t.Fatalf("formatters differ between runs")
	}
	if !reflect.DeepEqual(ae, be) {
		t.Fatalf("errs differ between runs")
	}
}
