package pipeline

import (
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
)

// stageKindsOf summarizes a Stage list by kind for compact assertions.
func stageKindsOf(stages []Stage) []StageKind {
	out := make([]StageKind, len(stages))
	for i, s := range stages {
		out[i] = s.Kind()
	}
	return out
}

func stageLevels(stages []Stage) []SourceLevel {
	out := make([]SourceLevel, len(stages))
	for i, s := range stages {
		out[i] = s.SourceLevel()
	}
	return out
}

func optOutKinds(opts []OptOut) []StageKind {
	out := make([]StageKind, len(opts))
	for i, o := range opts {
		out[i] = o.Kind
	}
	return out
}

// composeFromStmts builds a per-level entry list from raw stmts using
// composeLevel, asserting no errors. Helper for override-engine tests
// that already have valid input.
func composeOK(t *testing.T, stmts []ast.Stmt, level SourceLevel) []levelEntry {
	t.Helper()
	entries, errs := composeLevel(stmts, level)
	if len(errs) != 0 {
		t.Fatalf("composeLevel returned errors: %v", errs)
	}
	return entries
}

// --- Single-Instance Override ---

func TestOverrideSingleInstanceSystemOnly(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceSystem)
	stages, opts := buildEffectiveStages(sys, nil, nil, nil)
	if !reflect.DeepEqual(stageKindsOf(stages), []StageKind{StageApprove}) {
		t.Fatalf("kinds = %v, want [approve]", stageKindsOf(stages))
	}
	if stages[0].Span().Start.Line != 1 {
		t.Errorf("span line = %d, want 1 (system declaration)", stages[0].Span().Start.Line)
	}
	if stages[0].SourceLevel() != SourceSystem {
		t.Errorf("source = %v, want SourceSystem", stages[0].SourceLevel())
	}
	if len(opts) != 0 {
		t.Errorf("opts = %v, want empty", opts)
	}
}

func TestOverrideSingleInstanceGroupOverridesSystem(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceSystem)
	grp := composeOK(t, []ast.Stmt{mkApprove(src, 2)}, SourceGroup)
	gb := ast.NewGroupBlock(span(src, 2))
	stages, opts := buildEffectiveStages(sys, [][]levelEntry{grp}, []*ast.GroupBlock{gb}, nil)
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1", len(stages))
	}
	if stages[0].Span().Start.Line != 2 {
		t.Errorf("span line = %d, want 2 (group declaration)", stages[0].Span().Start.Line)
	}
	if stages[0].SourceLevel() != SourceGroup {
		t.Errorf("source = %v, want SourceGroup", stages[0].SourceLevel())
	}
	if stages[0].SourceGroup() != gb {
		t.Errorf("SourceGroup() did not match the originating block")
	}
	if len(opts) != 0 {
		t.Errorf("opts = %v, want empty", opts)
	}
}

func TestOverrideSingleInstanceHandlerWinsOverGroupAndSystem(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceSystem)
	grp := composeOK(t, []ast.Stmt{mkApprove(src, 2)}, SourceGroup)
	hnd := composeOK(t, []ast.Stmt{mkApprove(src, 3)}, SourceHandler)
	gb := ast.NewGroupBlock(span(src, 2))
	stages, _ := buildEffectiveStages(sys, [][]levelEntry{grp}, []*ast.GroupBlock{gb}, hnd)
	if stages[0].Span().Start.Line != 3 {
		t.Errorf("span line = %d, want 3 (handler declaration)", stages[0].Span().Start.Line)
	}
	if stages[0].SourceLevel() != SourceHandler {
		t.Errorf("source = %v, want SourceHandler", stages[0].SourceLevel())
	}
}

func TestOverrideSingleInstanceHandlerNoneRecordsOptOut(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{mkNone(src, 5, "approve")}, SourceHandler)
	stages, opts := buildEffectiveStages(sys, nil, nil, hnd)
	if len(stages) != 0 {
		t.Fatalf("stages = %v, want empty after handler-level approve none", stageKindsOf(stages))
	}
	if !reflect.DeepEqual(optOutKinds(opts), []StageKind{StageApprove}) {
		t.Fatalf("optOuts = %v, want [approve]", optOutKinds(opts))
	}
	if opts[0].Span.Start.Line != 5 {
		t.Errorf("opt-out span line = %d, want 5", opts[0].Span.Start.Line)
	}
}

func TestOverrideSingleInstanceLayoutNoneOptOut(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkLayout(src, 1)}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{mkNone(src, 5, "layout")}, SourceHandler)
	stages, opts := buildEffectiveStages(sys, nil, nil, hnd)
	if len(stages) != 0 {
		t.Fatalf("stages should be empty for layout none, got %v", stageKindsOf(stages))
	}
	if !reflect.DeepEqual(optOutKinds(opts), []StageKind{StageLayout}) {
		t.Fatalf("optOuts = %v, want [layout]", optOutKinds(opts))
	}
}

func TestOverrideSingleInstanceNoneClearedByLaterValue(t *testing.T) {
	// Handler: approve none; then approve again (within same level).
	// The second declaration should win and the opt-out should disappear.
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{
		mkNone(src, 4, "approve"),
		mkApprove(src, 5),
	}, SourceHandler)
	stages, opts := buildEffectiveStages(sys, nil, nil, hnd)
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1 (handler's approve wins)", len(stages))
	}
	if stages[0].Span().Start.Line != 5 {
		t.Errorf("span line = %d, want 5", stages[0].Span().Start.Line)
	}
	if len(opts) != 0 {
		t.Errorf("opt-outs should be empty after later value, got %v", opts)
	}
}

// --- Multi-Instance Composition ---

func TestOverrideMultiInstanceSystemThenHandler(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{mkResolve(src, 5)}, SourceHandler)
	stages, _ := buildEffectiveStages(sys, nil, nil, hnd)
	if !reflect.DeepEqual(stageKindsOf(stages), []StageKind{StageResolve, StageResolve}) {
		t.Fatalf("kinds = %v, want [resolve resolve]", stageKindsOf(stages))
	}
	if stages[0].Span().Start.Line != 1 || stages[1].Span().Start.Line != 5 {
		t.Errorf("lines = [%d %d], want [1 5]",
			stages[0].Span().Start.Line, stages[1].Span().Start.Line)
	}
	if stages[0].SourceLevel() != SourceSystem || stages[1].SourceLevel() != SourceHandler {
		t.Errorf("levels = [%v %v], want [system handler]",
			stages[0].SourceLevel(), stages[1].SourceLevel())
	}
}

func TestOverrideMultiInstanceThreeLevelLayering(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceSystem)
	grp := composeOK(t, []ast.Stmt{mkResolve(src, 2)}, SourceGroup)
	hnd := composeOK(t, []ast.Stmt{mkResolve(src, 3)}, SourceHandler)
	gb := ast.NewGroupBlock(span(src, 2))
	stages, _ := buildEffectiveStages(sys, [][]levelEntry{grp}, []*ast.GroupBlock{gb}, hnd)
	wantLines := []int{1, 2, 3}
	wantLevels := []SourceLevel{SourceSystem, SourceGroup, SourceHandler}
	for i, s := range stages {
		if s.Span().Start.Line != wantLines[i] {
			t.Errorf("stages[%d].Span line = %d, want %d", i, s.Span().Start.Line, wantLines[i])
		}
		if s.SourceLevel() != wantLevels[i] {
			t.Errorf("stages[%d].SourceLevel = %v, want %v", i, s.SourceLevel(), wantLevels[i])
		}
	}
}

func TestOverrideMultiInstanceTwoNestedGroups(t *testing.T) {
	src := newSrc("p.writ")
	gb1 := ast.NewGroupBlock(span(src, 1)) // less specific
	gb2 := ast.NewGroupBlock(span(src, 2)) // more specific
	g1 := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceGroup)
	g2 := composeOK(t, []ast.Stmt{mkResolve(src, 2)}, SourceGroup)
	stages, _ := buildEffectiveStages(nil,
		[][]levelEntry{g1, g2}, []*ast.GroupBlock{gb1, gb2}, nil)
	if len(stages) != 2 {
		t.Fatalf("len(stages) = %d, want 2", len(stages))
	}
	if stages[0].SourceGroup() != gb1 || stages[1].SourceGroup() != gb2 {
		t.Errorf("group order incorrect — less-specific group should appear first")
	}
}

func TestOverrideMultiInstanceWithinLevelSourceOrder(t *testing.T) {
	src := newSrc("p.writ")
	hnd := composeOK(t, []ast.Stmt{
		mkResolve(src, 1),
		mkResolve(src, 2),
		mkResolve(src, 3),
	}, SourceHandler)
	stages, _ := buildEffectiveStages(nil, nil, nil, hnd)
	if len(stages) != 3 {
		t.Fatalf("len(stages) = %d, want 3", len(stages))
	}
	for i, s := range stages {
		if s.Span().Start.Line != i+1 {
			t.Errorf("stages[%d].Span line = %d, want %d", i, s.Span().Start.Line, i+1)
		}
	}
}

func TestOverrideMultiInstanceNoneClearsInheritance(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceSystem)
	gb := ast.NewGroupBlock(span(src, 2))
	grp := composeOK(t, []ast.Stmt{mkResolve(src, 2)}, SourceGroup)
	hnd := composeOK(t, []ast.Stmt{
		mkNone(src, 5, "resolve"),
		mkResolve(src, 6),
	}, SourceHandler)
	stages, opts := buildEffectiveStages(sys, [][]levelEntry{grp}, []*ast.GroupBlock{gb}, hnd)
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1 (only handler's post-none resolve)", len(stages))
	}
	if stages[0].Span().Start.Line != 6 {
		t.Errorf("span line = %d, want 6", stages[0].Span().Start.Line)
	}
	if len(opts) != 0 {
		t.Errorf("opt-outs should be empty after later resolve cleared the none, got %v", opts)
	}
}

func TestOverrideMultiInstanceCommitsAndEmits(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{
		mkCommit(src, 1),
		mkEmit(src, 2),
	}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{
		mkCommit(src, 5),
		mkEmit(src, 6),
		mkFormat(src, 7),
	}, SourceHandler)
	stages, _ := buildEffectiveStages(sys, nil, nil, hnd)
	want := []StageKind{StageCommit, StageCommit, StageEmit, StageEmit, StageFormat}
	if !reflect.DeepEqual(stageKindsOf(stages), want) {
		t.Fatalf("kinds = %v, want %v", stageKindsOf(stages), want)
	}
}

// --- Observational Composition ---

func TestOverrideObservationalAcrossLevels(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkLog(src, 1)}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{
		mkLog(src, 5),
		mkApprove(src, 6),
	}, SourceHandler)
	stages, _ := buildEffectiveStages(sys, nil, nil, hnd)
	want := []StageKind{StageLog, StageLog, StageApprove}
	if !reflect.DeepEqual(stageKindsOf(stages), want) {
		t.Fatalf("kinds = %v, want %v", stageKindsOf(stages), want)
	}
	wantLevels := []SourceLevel{SourceSystem, SourceHandler, SourceHandler}
	if !reflect.DeepEqual(stageLevels(stages), wantLevels) {
		t.Fatalf("levels = %v, want %v", stageLevels(stages), wantLevels)
	}
}

func TestOverrideObservationalInterleavedWithResolves(t *testing.T) {
	// The spec example: log + approve + resolve + log + resolve + log + format
	src := newSrc("p.writ")
	hnd := composeOK(t, []ast.Stmt{
		mkLog(src, 1),
		mkApprove(src, 2),
		mkResolve(src, 3),
		mkLog(src, 4),
		mkResolve(src, 5),
		mkLog(src, 6),
		mkFormat(src, 7),
	}, SourceHandler)
	stages, _ := buildEffectiveStages(nil, nil, nil, hnd)
	want := []StageKind{
		StageLog, StageApprove,
		StageResolve, StageLog, StageResolve, StageLog,
		StageFormat,
	}
	if !reflect.DeepEqual(stageKindsOf(stages), want) {
		t.Fatalf("kinds = %v, want %v", stageKindsOf(stages), want)
	}
}

func TestOverrideObservationalNoneClearsAcrossLevels(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{
		mkLog(src, 1),
		mkLog(src, 2),
	}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{
		mkNone(src, 5, "log"),
		mkLog(src, 6),
	}, SourceHandler)
	stages, opts := buildEffectiveStages(sys, nil, nil, hnd)
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1 (only handler's post-none log)", len(stages))
	}
	if stages[0].Span().Start.Line != 6 {
		t.Errorf("span line = %d, want 6", stages[0].Span().Start.Line)
	}
	if len(opts) != 0 {
		t.Errorf("opt-outs should be empty after later log cleared the none, got %v", opts)
	}
}

// --- Nested Group Layering (NestedGroupLayering acceptance section) ---

func TestOverrideNestedGroupsSingleInstanceMostSpecificWins(t *testing.T) {
	// Outer group declares approve_X; inner group declares approve_Y.
	// Inner's wins.
	src := newSrc("p.writ")
	gb1 := ast.NewGroupBlock(span(src, 1))
	gb2 := ast.NewGroupBlock(span(src, 2))
	g1 := composeOK(t, []ast.Stmt{mkApprove(src, 1)}, SourceGroup)
	g2 := composeOK(t, []ast.Stmt{mkApprove(src, 2)}, SourceGroup)
	stages, _ := buildEffectiveStages(nil,
		[][]levelEntry{g1, g2}, []*ast.GroupBlock{gb1, gb2}, nil)
	if len(stages) != 1 {
		t.Fatalf("len(stages) = %d, want 1 (only inner approve survives)", len(stages))
	}
	if stages[0].SourceGroup() != gb2 {
		t.Errorf("approve should come from the more-specific (inner) group")
	}
}

func TestOverrideNestedGroupsLessSpecificFillsGap(t *testing.T) {
	// Outer group declares limit; inner group does not declare limit.
	// Outer's limit survives in the effective pipeline.
	src := newSrc("p.writ")
	gb1 := ast.NewGroupBlock(span(src, 1))
	gb2 := ast.NewGroupBlock(span(src, 2))
	g1 := composeOK(t, []ast.Stmt{mkLimit(src, 1)}, SourceGroup)
	g2 := composeOK(t, []ast.Stmt{mkApprove(src, 2)}, SourceGroup)
	stages, _ := buildEffectiveStages(nil,
		[][]levelEntry{g1, g2}, []*ast.GroupBlock{gb1, gb2}, nil)
	want := []StageKind{StageLimit, StageApprove}
	if !reflect.DeepEqual(stageKindsOf(stages), want) {
		t.Fatalf("kinds = %v, want %v", stageKindsOf(stages), want)
	}
	if stages[0].SourceGroup() != gb1 {
		t.Errorf("limit should come from the less-specific group")
	}
}

func TestOverrideNestedGroupsMultiInstanceOrder(t *testing.T) {
	// Less-specific group resolves come before more-specific group resolves,
	// which come before handler resolves.
	src := newSrc("p.writ")
	gb1 := ast.NewGroupBlock(span(src, 1))
	gb2 := ast.NewGroupBlock(span(src, 2))
	g1 := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceGroup)
	g2 := composeOK(t, []ast.Stmt{mkResolve(src, 2)}, SourceGroup)
	hnd := composeOK(t, []ast.Stmt{mkResolve(src, 3)}, SourceHandler)
	stages, _ := buildEffectiveStages(nil,
		[][]levelEntry{g1, g2}, []*ast.GroupBlock{gb1, gb2}, hnd)
	if len(stages) != 3 {
		t.Fatalf("len(stages) = %d, want 3", len(stages))
	}
	wantGroups := []*ast.GroupBlock{gb1, gb2, nil}
	wantLevels := []SourceLevel{SourceGroup, SourceGroup, SourceHandler}
	for i, s := range stages {
		if s.SourceGroup() != wantGroups[i] {
			t.Errorf("stages[%d].SourceGroup mismatch", i)
		}
		if s.SourceLevel() != wantLevels[i] {
			t.Errorf("stages[%d].SourceLevel = %v, want %v", i, s.SourceLevel(), wantLevels[i])
		}
	}
}

// --- Determinism ---

func TestOverrideDeterministic(t *testing.T) {
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{
		mkLog(src, 1),
		mkApprove(src, 2),
		mkResolve(src, 3),
	}, SourceSystem)
	hnd := composeOK(t, []ast.Stmt{
		mkResolve(src, 5),
		mkLog(src, 6),
		mkFormat(src, 7),
	}, SourceHandler)
	a1, o1 := buildEffectiveStages(sys, nil, nil, hnd)
	a2, o2 := buildEffectiveStages(sys, nil, nil, hnd)
	if !reflect.DeepEqual(stageKindsOf(a1), stageKindsOf(a2)) {
		t.Fatalf("kinds differ: %v vs %v", stageKindsOf(a1), stageKindsOf(a2))
	}
	if !reflect.DeepEqual(o1, o2) {
		t.Fatalf("opts differ: %v vs %v", o1, o2)
	}
}

func TestOverrideEmptyEverything(t *testing.T) {
	stages, opts := buildEffectiveStages(nil, nil, nil, nil)
	if len(stages) != 0 || len(opts) != 0 {
		t.Fatalf("stages=%v opts=%v, want both empty", stages, opts)
	}
}
