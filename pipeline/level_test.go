package pipeline

import (
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
)

// span builds a synthetic span anchored at a 1-based line number on a
// single-source file. Tests only inspect span.Start.Line for ordering;
// columns and offsets are unused.
func span(src *ast.Source, line int) ast.Span {
	pos := ast.Position{Source: src, Line: line, Column: 1, Offset: line}
	return ast.Span{Start: pos, End: pos}
}

func newSrc(name string) *ast.Source {
	return &ast.Source{Path: name, Bytes: nil}
}

// Tiny statement constructors with explicit-line spans.

func mkLog(src *ast.Source, line int) ast.Stmt {
	return ast.NewLogStmt(span(src, line))
}

func mkMeasure(src *ast.Source, line int) ast.Stmt {
	return ast.NewMeasureStmt(span(src, line))
}

func mkSession(src *ast.Source, line int) ast.Stmt {
	return ast.NewSessionStmt(span(src, line))
}

func mkCSRF(src *ast.Source, line int) ast.Stmt {
	return ast.NewCSRFStmt(span(src, line))
}

func mkLimit(src *ast.Source, line int) ast.Stmt {
	return ast.NewLimitStmt(span(src, line))
}

func mkApprove(src *ast.Source, line int) ast.Stmt {
	return ast.NewApproveStmt(span(src, line))
}

func mkResolve(src *ast.Source, line int) ast.Stmt {
	return ast.NewResolveStmt(span(src, line))
}

func mkCommit(src *ast.Source, line int) ast.Stmt {
	return ast.NewCommitStmt(span(src, line))
}

func mkEmit(src *ast.Source, line int) ast.Stmt {
	return ast.NewEmitStmt(span(src, line))
}

func mkLayout(src *ast.Source, line int) ast.Stmt {
	return ast.NewLayoutStmt(span(src, line))
}

func mkFormat(src *ast.Source, line int) ast.Stmt {
	return ast.NewFormatStmt(span(src, line))
}

func mkRedirect(src *ast.Source, line int) ast.Stmt {
	return ast.NewRedirectStmt(span(src, line))
}

func mkNone(src *ast.Source, line int, stage string) ast.Stmt {
	n := ast.NewNoneStmt(span(src, line))
	n.Stage = stage
	return n
}

// kindsOf extracts the StageKind sequence from a list of level entries
// for compact assertions.
func kindsOf(entries []levelEntry) []StageKind {
	out := make([]StageKind, len(entries))
	for i, e := range entries {
		out[i] = e.kind
	}
	return out
}

func TestComposeLevelEmpty(t *testing.T) {
	entries, errs := composeLevel(nil, SourceHandler)
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", entries)
	}
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want empty", errs)
	}
}

func TestComposeLevelCanonicalHandler(t *testing.T) {
	src := newSrc("h.writ")
	stmts := []ast.Stmt{
		mkSession(src, 1),
		mkCSRF(src, 2),
		mkLimit(src, 3),
		mkApprove(src, 4),
		mkResolve(src, 5),
		mkCommit(src, 6),
		mkEmit(src, 7),
		mkLayout(src, 8),
		mkFormat(src, 9),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	want := []StageKind{
		StageSession, StageCSRF, StageLimit, StageApprove,
		StageResolve, StageCommit, StageEmit, StageLayout, StageFormat,
	}
	if !reflect.DeepEqual(kindsOf(entries), want) {
		t.Fatalf("kinds = %v, want %v", kindsOf(entries), want)
	}
}

func TestComposeLevelNonCanonicalHandlerReportsError(t *testing.T) {
	src := newSrc("h.writ")
	// approve appears before csrf — non-canonical.
	stmts := []ast.Stmt{
		mkSession(src, 1),
		mkApprove(src, 2),
		mkCSRF(src, 3),
		mkFormat(src, 4),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 1 {
		t.Fatalf("expected 1 stage-order error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != StageOrder {
		t.Errorf("Kind = %v, want StageOrder", errs[0].Kind)
	}
	if errs[0].Span.Start.Line != 3 {
		t.Errorf("error line = %d, want 3 (the csrf line)", errs[0].Span.Start.Line)
	}

	// Despite the order violation, both approve and csrf are still placed
	// in canonical order in the entry list.
	want := []StageKind{StageSession, StageCSRF, StageApprove, StageFormat}
	if !reflect.DeepEqual(kindsOf(entries), want) {
		t.Fatalf("kinds = %v, want %v (canonical order preserved)", kindsOf(entries), want)
	}
}

func TestComposeLevelObservationalsExemptFromOrderCheck(t *testing.T) {
	src := newSrc("h.writ")
	// log between two resolves; no order error since log is observational.
	stmts := []ast.Stmt{
		mkApprove(src, 1),
		mkResolve(src, 2),
		mkLog(src, 3),
		mkResolve(src, 4),
		mkLog(src, 5),
		mkFormat(src, 6),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	want := []StageKind{
		StageApprove,
		StageResolve, StageLog, StageResolve, StageLog,
		StageFormat,
	}
	if !reflect.DeepEqual(kindsOf(entries), want) {
		t.Fatalf("kinds = %v, want %v", kindsOf(entries), want)
	}
}

func TestComposeLevelObservationalBeforeAnySemantic(t *testing.T) {
	src := newSrc("h.writ")
	stmts := []ast.Stmt{
		mkLog(src, 1),
		mkMeasure(src, 2),
		mkApprove(src, 3),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	want := []StageKind{StageLog, StageMeasure, StageApprove}
	if !reflect.DeepEqual(kindsOf(entries), want) {
		t.Fatalf("kinds = %v, want %v", kindsOf(entries), want)
	}
}

func TestComposeLevelFormatInSystemBlockIsPlacementError(t *testing.T) {
	src := newSrc("s.writ")
	stmts := []ast.Stmt{
		mkApprove(src, 1),
		mkFormat(src, 2),
	}
	entries, errs := composeLevel(stmts, SourceSystem)
	if len(errs) != 1 {
		t.Fatalf("expected 1 placement error, got %d: %v", len(errs), errs)
	}
	if errs[0].Kind != StagePlacement {
		t.Errorf("Kind = %v, want StagePlacement", errs[0].Kind)
	}
	if errs[0].Span.Start.Line != 2 {
		t.Errorf("error line = %d, want 2", errs[0].Span.Start.Line)
	}
	// format is dropped from entries.
	if !reflect.DeepEqual(kindsOf(entries), []StageKind{StageApprove}) {
		t.Fatalf("kinds = %v, want [approve]", kindsOf(entries))
	}
}

func TestComposeLevelRedirectInGroupIsPlacementError(t *testing.T) {
	src := newSrc("g.writ")
	stmts := []ast.Stmt{mkRedirect(src, 1)}
	entries, errs := composeLevel(stmts, SourceGroup)
	if len(errs) != 1 {
		t.Fatalf("expected 1 placement error, got %d", len(errs))
	}
	if errs[0].Kind != StagePlacement {
		t.Errorf("Kind = %v, want StagePlacement", errs[0].Kind)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty (redirect dropped)", kindsOf(entries))
	}
}

func TestComposeLevelFormatNoneIsPlacementErrorAtHandler(t *testing.T) {
	src := newSrc("h.writ")
	stmts := []ast.Stmt{mkNone(src, 1, "format")}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 1 {
		t.Fatalf("expected 1 placement error, got %d", len(errs))
	}
	if errs[0].Kind != StagePlacement {
		t.Errorf("Kind = %v, want StagePlacement", errs[0].Kind)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty (format none dropped)", kindsOf(entries))
	}
}

func TestComposeLevelRedirectNoneIsPlacementErrorAtSystem(t *testing.T) {
	// At system level, redirect without none is already a placement
	// violation; redirect none should also error. The error reports the
	// level violation.
	src := newSrc("s.writ")
	stmts := []ast.Stmt{mkNone(src, 1, "redirect")}
	entries, errs := composeLevel(stmts, SourceSystem)
	if len(errs) != 1 {
		t.Fatalf("expected 1 placement error, got %d", len(errs))
	}
	if errs[0].Kind != StagePlacement {
		t.Errorf("Kind = %v, want StagePlacement", errs[0].Kind)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty (redirect none dropped)", kindsOf(entries))
	}
}

func TestComposeLevelMultipleViolationsReportedInOnePass(t *testing.T) {
	src := newSrc("h.writ")
	stmts := []ast.Stmt{
		mkApprove(src, 1),
		mkCSRF(src, 2), // stage-order error (csrf after approve)
		mkResolve(src, 3),
		mkSession(src, 4),        // stage-order error (session after resolve)
		mkNone(src, 5, "format"), // placement error
		mkFormat(src, 6),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 3 {
		t.Fatalf("expected 3 errors, got %d: %v", len(errs), errs)
	}
	wantKinds := []ErrorKind{StageOrder, StageOrder, StagePlacement}
	for i, e := range errs {
		if e.Kind != wantKinds[i] {
			t.Errorf("errs[%d].Kind = %v, want %v", i, e.Kind, wantKinds[i])
		}
	}
	// All semantic stages remain at canonical positions.
	wantEntries := []StageKind{
		StageSession, StageCSRF, StageApprove, StageResolve, StageFormat,
	}
	if !reflect.DeepEqual(kindsOf(entries), wantEntries) {
		t.Fatalf("kinds = %v, want %v", kindsOf(entries), wantEntries)
	}
}

func TestComposeLevelDeterministic(t *testing.T) {
	src := newSrc("h.writ")
	stmts := []ast.Stmt{
		mkLog(src, 1),
		mkApprove(src, 2),
		mkResolve(src, 3),
		mkLog(src, 4),
		mkFormat(src, 5),
	}
	a, errA := composeLevel(stmts, SourceHandler)
	b, errB := composeLevel(stmts, SourceHandler)
	if !reflect.DeepEqual(kindsOf(a), kindsOf(b)) {
		t.Fatalf("kinds differ between calls: %v vs %v", kindsOf(a), kindsOf(b))
	}
	if !reflect.DeepEqual(errA, errB) {
		t.Fatalf("errs differ between calls: %v vs %v", errA, errB)
	}
}

func TestComposeLevelNoneStmtForSemanticStage(t *testing.T) {
	src := newSrc("h.writ")
	// approve none at handler level — valid composition opt-out, placed
	// at slot 4 with isNone=true.
	stmts := []ast.Stmt{
		mkSession(src, 1),
		mkNone(src, 2, "approve"),
		mkFormat(src, 3),
	}
	entries, errs := composeLevel(stmts, SourceHandler)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(entries) != 3 {
		t.Fatalf("entries len = %d, want 3", len(entries))
	}
	if entries[1].kind != StageApprove || !entries[1].isNone {
		t.Errorf("entries[1] = %+v, want approve with isNone=true", entries[1])
	}
}
