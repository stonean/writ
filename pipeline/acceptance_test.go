package pipeline

import (
	"testing"
	"testing/fstest"

	"github.com/stonean/writ/ast"
	"github.com/stonean/writ/parser"
)

// acceptance_test.go covers every checkbox under "Acceptance Criteria"
// in specs/002-pipeline-elaboration/spec.md. Tests are grouped by spec
// section so a reader can audit coverage by reading top-to-bottom
// against the spec.
//
// Tests round-trip through parser.ParseString (or parser.Parse for
// include-flatten coverage) so the elaborator only assumes shapes the
// real parser produces.

// --- helpers ---

// elabOK parses src and elaborates the resulting program, failing the
// test on any parser or elaborator error.
func elabOK(t *testing.T, path, src string) *Resolved {
	t.Helper()
	prog, perrs := parser.ParseString(path, src)
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	resolved, errs := Elaborate(prog)
	if len(errs) != 0 {
		t.Fatalf("elaborate errors: %v", errs)
	}
	return resolved
}

// elab parses src and elaborates, failing the test on parse errors but
// returning whatever elaborate errors occurred.
func elab(t *testing.T, path, src string) (*Resolved, []Error) {
	t.Helper()
	prog, perrs := parser.ParseString(path, src)
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	return Elaborate(prog)
}

// firstStageOfKind returns the first stage of kind in stages, or nil.
func firstStageOfKind(stages []Stage, kind StageKind) Stage {
	for _, s := range stages {
		if s.Kind() == kind {
			return s
		}
	}
	return nil
}

// hasOptOut reports whether opts contains an OptOut for kind.
func hasOptOut(opts []OptOut, kind StageKind) bool {
	for _, o := range opts {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

// hasErrorKind reports whether errs contains an Error of kind.
func hasErrorKind(errs []Error, kind ErrorKind) bool {
	for _, e := range errs {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// countErrorKind returns the number of Errors of kind in errs.
func countErrorKind(errs []Error, kind ErrorKind) int {
	n := 0
	for _, e := range errs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// =====================================================================
// Single-Instance Override
// =====================================================================

func TestAcceptanceSingleInstanceSystemInheritance(t *testing.T) {
	src := `system ->
  approve auth.X

GET /users/:id ->
  log :id
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	h := r.Handlers[0]
	approve := firstStageOfKind(h.Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve stage")
	}
	if approve.SourceLevel() != SourceSystem {
		t.Errorf("source = %v, want SourceSystem", approve.SourceLevel())
	}
	if approve.Span().Start.Line != 2 {
		t.Errorf("span line = %d, want 2 (system declaration)", approve.Span().Start.Line)
	}
}

func TestAcceptanceSingleInstanceGroupInheritance(t *testing.T) {
	src := `group /users/* ->
  approve auth.Y

GET /users/:id ->
  log :id
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	h := r.Handlers[0]
	approve := firstStageOfKind(h.Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve stage")
	}
	if approve.SourceLevel() != SourceGroup {
		t.Errorf("source = %v, want SourceGroup", approve.SourceLevel())
	}
	if approve.Span().Start.Line != 2 {
		t.Errorf("span line = %d, want 2 (group declaration)", approve.Span().Start.Line)
	}
	if approve.SourceGroup() == nil {
		t.Errorf("SourceGroup = nil, want non-nil")
	}
}

func TestAcceptanceSingleInstanceHandlerOverride(t *testing.T) {
	src := `system ->
  approve auth.X

group /users/* ->
  approve auth.Y

GET /users/:id ->
  approve auth.Z
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	h := r.Handlers[0]
	approve := firstStageOfKind(h.Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve stage")
	}
	if approve.SourceLevel() != SourceHandler {
		t.Errorf("source = %v, want SourceHandler", approve.SourceLevel())
	}
	if approve.Span().Start.Line != 8 {
		t.Errorf("span line = %d, want 8 (handler declaration)", approve.Span().Start.Line)
	}
}

func TestAcceptanceSingleInstanceHandlerNoneOptOut(t *testing.T) {
	src := `system ->
  approve auth.X

GET /users/:id ->
  approve none
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	h := r.Handlers[0]
	if approve := firstStageOfKind(h.Stages, StageApprove); approve != nil {
		t.Errorf("expected no approve stage, got %v", approve)
	}
	if !hasOptOut(h.OptOuts, StageApprove) {
		t.Errorf("expected approve OptOut, got %v", h.OptOuts)
	}
}

func TestAcceptanceSingleInstanceNoneDistinctFromUndeclared(t *testing.T) {
	// "approve none" → recorded as OptOut.
	// no approve at any level → no entry in either Stages or OptOuts.
	srcDeclared := `GET /users/:id ->
  approve none
  format u.html with x
`
	srcUndeclared := `GET /users/:id ->
  log :id
  format u.html with x
`
	rd := elabOK(t, "p.writ", srcDeclared)
	if !hasOptOut(rd.Handlers[0].OptOuts, StageApprove) {
		t.Errorf("explicit approve none should record OptOut")
	}
	ru := elabOK(t, "p.writ", srcUndeclared)
	if hasOptOut(ru.Handlers[0].OptOuts, StageApprove) {
		t.Errorf("undeclared approve should not record OptOut")
	}
	if firstStageOfKind(ru.Handlers[0].Stages, StageApprove) != nil {
		t.Errorf("undeclared approve should not appear in Stages")
	}
}

func TestAcceptanceSingleInstanceEveryKind(t *testing.T) {
	// Verify the same override semantics for every single-instance
	// kind: session, csrf, limit, approve, layout. (log and measure
	// are observational/multi-instance — they appear in this section
	// of the spec but are tested under multi-instance composition.)
	src := `system ->
  session cookie
  csrf auto
  limit rate.limit()
  approve auth.A
  layout app

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	h := r.Handlers[0]
	for _, kind := range []StageKind{StageSession, StageCSRF, StageLimit, StageApprove, StageLayout} {
		s := firstStageOfKind(h.Stages, kind)
		if s == nil {
			t.Errorf("missing %s stage", kind)
			continue
		}
		if s.SourceLevel() != SourceSystem {
			t.Errorf("%s SourceLevel = %v, want SourceSystem", kind, s.SourceLevel())
		}
	}
}

func TestAcceptanceLayoutNoneOptOut(t *testing.T) {
	// layout none records an explicit opt-out distinct from "no
	// layout declared at any level".
	srcOptOut := `system ->
  layout app

GET /users/:id ->
  layout none
  format u.html with x
`
	srcUndeclared := `GET /users/:id ->
  log :id
  format u.html with x
`
	rOpt := elabOK(t, "p.writ", srcOptOut)
	hOpt := rOpt.Handlers[0]
	if firstStageOfKind(hOpt.Stages, StageLayout) != nil {
		t.Errorf("layout none should clear stage")
	}
	if !hasOptOut(hOpt.OptOuts, StageLayout) {
		t.Errorf("layout none should record OptOut")
	}

	rNone := elabOK(t, "p.writ", srcUndeclared)
	if hasOptOut(rNone.Handlers[0].OptOuts, StageLayout) {
		t.Errorf("undeclared layout should not record OptOut")
	}
}

// =====================================================================
// Multi-Instance Composition
// =====================================================================

func TestAcceptanceMultiInstanceSystemPlusHandler(t *testing.T) {
	src := `system ->
  resolve a = X.foo()

GET /users/:id ->
  resolve b = X.bar()
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	resolves := stagesOfKind(r.Handlers[0].Stages, StageResolve)
	if len(resolves) != 2 {
		t.Fatalf("resolves = %d, want 2", len(resolves))
	}
	if resolves[0].(*ResolveStage).Name() != "a" || resolves[1].(*ResolveStage).Name() != "b" {
		t.Errorf("got [%s, %s], want [a, b]",
			resolves[0].(*ResolveStage).Name(), resolves[1].(*ResolveStage).Name())
	}
}

func TestAcceptanceMultiInstanceThreeLevelLayering(t *testing.T) {
	src := `system ->
  resolve sysstep = X.sys()

group /users/* ->
  resolve grpstep = X.grp()

GET /users/:id ->
  resolve hndstep = X.hnd()
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	resolves := stagesOfKind(r.Handlers[0].Stages, StageResolve)
	if len(resolves) != 3 {
		t.Fatalf("resolves = %d, want 3", len(resolves))
	}
	wantNames := []string{"sysstep", "grpstep", "hndstep"}
	wantLevels := []SourceLevel{SourceSystem, SourceGroup, SourceHandler}
	for i, s := range resolves {
		if name := s.(*ResolveStage).Name(); name != wantNames[i] {
			t.Errorf("resolves[%d] name = %q, want %q", i, name, wantNames[i])
		}
		if s.SourceLevel() != wantLevels[i] {
			t.Errorf("resolves[%d] level = %v, want %v", i, s.SourceLevel(), wantLevels[i])
		}
	}
}

func TestAcceptanceMultiInstanceTwoGroupContainmentOrdering(t *testing.T) {
	src := `group /admin/* ->
  resolve outer = X.outer()

group /admin/users/* ->
  resolve inner = X.inner()

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	resolves := stagesOfKind(r.Handlers[0].Stages, StageResolve)
	if len(resolves) != 2 {
		t.Fatalf("resolves = %d, want 2", len(resolves))
	}
	if resolves[0].(*ResolveStage).Name() != "outer" {
		t.Errorf("first resolve name = %q, want outer (less-specific group)",
			resolves[0].(*ResolveStage).Name())
	}
	if resolves[1].(*ResolveStage).Name() != "inner" {
		t.Errorf("second resolve name = %q, want inner (more-specific group)",
			resolves[1].(*ResolveStage).Name())
	}
}

func TestAcceptanceMultiInstanceSourceOrderWithinLevel(t *testing.T) {
	src := `GET /users/:id ->
  resolve a = X.a()
  resolve b = X.b()
  resolve c = X.c()
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	resolves := stagesOfKind(r.Handlers[0].Stages, StageResolve)
	if len(resolves) != 3 {
		t.Fatalf("resolves = %d, want 3", len(resolves))
	}
	for i, want := range []string{"a", "b", "c"} {
		if got := resolves[i].(*ResolveStage).Name(); got != want {
			t.Errorf("resolves[%d] name = %q, want %q", i, got, want)
		}
	}
}

func TestAcceptanceMultiInstanceNoneClearsInherited(t *testing.T) {
	// The current parser doesn't accept `resolve none` as a token sequence
	// (parseResolveStmt expects `<name> = <call>`), so this acceptance
	// criterion is verified at the AST level. The override engine's
	// behavior is also covered by TestOverrideMultiInstanceNoneClearsInheritance
	// in override_test.go.
	src := newSrc("p.writ")
	sys := composeOK(t, []ast.Stmt{mkResolve(src, 1)}, SourceSystem)
	grp := composeOK(t, []ast.Stmt{mkResolve(src, 2)}, SourceGroup)
	gb := ast.NewGroupBlock(span(src, 2))
	hnd := composeOK(t, []ast.Stmt{
		mkNone(src, 5, "resolve"),
		mkResolve(src, 6),
	}, SourceHandler)
	stages, _ := buildEffectiveStages(sys, [][]levelEntry{grp}, []*ast.GroupBlock{gb}, hnd)
	resolves := stagesOfKind(stages, StageResolve)
	if len(resolves) != 1 {
		t.Fatalf("resolves = %d, want 1 (only post-none handler resolve)", len(resolves))
	}
	if resolves[0].Span().Start.Line != 6 {
		t.Errorf("surviving resolve span line = %d, want 6", resolves[0].Span().Start.Line)
	}
}

func TestAcceptanceMultiInstanceCommitAndEmitFollowSameRules(t *testing.T) {
	src := `system ->
  commit syssig = X.sys()
  emit user.created

group /users/* ->
  commit grpsig = X.grp()
  emit audit.touched with payload

GET /users/:id ->
  commit hndsig = X.hnd()
  emit handler.done
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	commits := stagesOfKind(r.Handlers[0].Stages, StageCommit)
	emits := stagesOfKind(r.Handlers[0].Stages, StageEmit)
	if len(commits) != 3 {
		t.Errorf("commits = %d, want 3", len(commits))
	}
	if len(emits) != 3 {
		t.Errorf("emits = %d, want 3", len(emits))
	}
	wantLevels := []SourceLevel{SourceSystem, SourceGroup, SourceHandler}
	for i, s := range commits {
		if s.SourceLevel() != wantLevels[i] {
			t.Errorf("commits[%d].SourceLevel = %v, want %v", i, s.SourceLevel(), wantLevels[i])
		}
	}
	for i, s := range emits {
		if s.SourceLevel() != wantLevels[i] {
			t.Errorf("emits[%d].SourceLevel = %v, want %v", i, s.SourceLevel(), wantLevels[i])
		}
	}
}

func TestAcceptanceObservationalsComposeAndFloat(t *testing.T) {
	// Observational stages compose across levels and float to source
	// position relative to surrounding semantic stages.
	src := `system ->
  log :id

GET /users/:id ->
  log :id
  approve auth.isOwner
  resolve user = db.users(:id)
  log :user.id
  resolve posts = db.posts(:user.id)
  log :user.id
  format u.html with user, posts
`
	r := elabOK(t, "p.writ", src)
	stages := r.Handlers[0].Stages
	// Expected sequence:
	//   log (system, before everything),
	//   log (handler, before approve),
	//   approve,
	//   resolve user,
	//   log (after resolve user),
	//   resolve posts,
	//   log (after resolve posts),
	//   format
	wantKinds := []StageKind{
		StageLog, StageLog, StageApprove,
		StageResolve, StageLog, StageResolve, StageLog,
		StageFormat,
	}
	if got := stageKindsOf(stages); !equalKinds(got, wantKinds) {
		t.Errorf("kinds = %v\nwant   %v", got, wantKinds)
	}
	// First log is system-level, second is handler-level (before approve).
	if stages[0].SourceLevel() != SourceSystem {
		t.Errorf("stages[0] level = %v, want SourceSystem", stages[0].SourceLevel())
	}
	if stages[1].SourceLevel() != SourceHandler {
		t.Errorf("stages[1] level = %v, want SourceHandler", stages[1].SourceLevel())
	}
}

// =====================================================================
// Group Membership
// =====================================================================

func TestAcceptanceGroupMembershipContainmentMatch(t *testing.T) {
	src := `group /admin/* ->
  approve auth.isAdmin

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil || approve.SourceLevel() != SourceGroup {
		t.Errorf("expected approve from group, got %v", approve)
	}
}

func TestAcceptanceGroupMembershipNonMatch(t *testing.T) {
	src := `group /admin/* ->
  approve auth.isAdmin

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	if firstStageOfKind(r.Handlers[0].Stages, StageApprove) != nil {
		t.Errorf("/users/:id should not inherit from /admin/*")
	}
}

func TestAcceptanceGroupMembershipNoGroupOnlySystem(t *testing.T) {
	src := `system ->
  approve auth.isUser

GET /standalone ->
  format s.html with x
`
	r := elabOK(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil || approve.SourceLevel() != SourceSystem {
		t.Errorf("expected approve from system, got %v", approve)
	}
}

func TestAcceptanceGroupMembershipParameterSegmentContainment(t *testing.T) {
	// /users/:id/* matches /users/:id/posts because :id contains any
	// literal segment value.
	src := `group /users/:id/* ->
  approve auth.canAccessUser

GET /users/:id/posts ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil || approve.SourceLevel() != SourceGroup {
		t.Errorf("expected approve from group, got %v", approve)
	}
}

// =====================================================================
// Nested Group Layering
// =====================================================================

func TestAcceptanceNestedGroupSingleInstanceMoreSpecificWins(t *testing.T) {
	src := `group /admin/* ->
  approve auth.isAdmin

group /admin/users/* ->
  approve auth.canManageUsers

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve")
	}
	if approve.Span().Start.Line != 5 {
		t.Errorf("approve span line = %d, want 5 (more-specific group)",
			approve.Span().Start.Line)
	}
}

func TestAcceptanceNestedGroupLessSpecificFillsGaps(t *testing.T) {
	src := `group /admin/* ->
  csrf auto
  approve auth.isAdmin

group /admin/users/* ->
  approve auth.canManageUsers

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	csrf := firstStageOfKind(r.Handlers[0].Stages, StageCSRF)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if csrf == nil {
		t.Errorf("csrf from less-specific group should be preserved")
	}
	if approve == nil || approve.Span().Start.Line != 6 {
		t.Errorf("approve should come from more-specific group at line 6, got line %d",
			lineOf(approve))
	}
}

func lineOf(s Stage) int {
	if s == nil {
		return -1
	}
	return s.Span().Start.Line
}

func TestAcceptanceNestedGroupMultiInstanceOrdering(t *testing.T) {
	src := `group /admin/* ->
  resolve outer = X.outer()

group /admin/users/* ->
  resolve middle = X.middle()

GET /admin/users/:id ->
  resolve inner = X.inner()
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	resolves := stagesOfKind(r.Handlers[0].Stages, StageResolve)
	if len(resolves) != 3 {
		t.Fatalf("resolves = %d, want 3", len(resolves))
	}
	wantNames := []string{"outer", "middle", "inner"}
	for i, want := range wantNames {
		if got := resolves[i].(*ResolveStage).Name(); got != want {
			t.Errorf("resolves[%d] = %q, want %q", i, got, want)
		}
	}
}

// =====================================================================
// Errors Block Selection
// =====================================================================

func TestAcceptanceErrorsBlockLayered(t *testing.T) {
	src := `errors /* ->
  ServerError serverErrJSON
  default defaultJSON

errors /admin/* ->
  NotFound notFoundJSON

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	em := r.Handlers[0].ErrorMap
	gotTypes := map[string]string{}
	for _, e := range em {
		gotTypes[e.TypeName] = e.Formatter
	}
	if gotTypes["NotFound"] != "notFoundJSON" {
		t.Errorf("NotFound formatter = %q, want notFoundJSON (from /admin/*)", gotTypes["NotFound"])
	}
	if gotTypes["ServerError"] != "serverErrJSON" {
		t.Errorf("ServerError formatter = %q, want serverErrJSON (from /*)", gotTypes["ServerError"])
	}
	if gotTypes["default"] != "defaultJSON" {
		t.Errorf("default formatter = %q, want defaultJSON (from /*)", gotTypes["default"])
	}
}

func TestAcceptanceErrorsBlockMostSpecificWinsPerType(t *testing.T) {
	src := `errors /* ->
  NotFound globalNotFoundJSON

errors /admin/* ->
  NotFound adminNotFoundJSON

GET /admin/users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	em := r.Handlers[0].ErrorMap
	for _, e := range em {
		if e.TypeName == "NotFound" {
			if e.Formatter != "adminNotFoundJSON" {
				t.Errorf("NotFound formatter = %q, want adminNotFoundJSON", e.Formatter)
			}
			return
		}
	}
	t.Errorf("expected NotFound entry, got %v", em)
}

func TestAcceptanceErrorsBlockNoMatchEmpty(t *testing.T) {
	src := `errors /admin/* ->
  NotFound notFoundJSON

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	if len(r.Handlers[0].ErrorMap) != 0 {
		t.Errorf("ErrorMap = %v, want empty", r.Handlers[0].ErrorMap)
	}
}

func TestAcceptanceErrorsBlockReachableFromHandler(t *testing.T) {
	// The handler's ErrorMap must be reachable without re-walking the
	// AST: each entry's SourceBlock points at the originating block.
	src := `errors /* ->
  NotFound notFoundJSON

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	em := r.Handlers[0].ErrorMap
	if len(em) == 0 {
		t.Fatalf("ErrorMap is empty")
	}
	if em[0].SourceBlock == nil {
		t.Errorf("ErrorMap[0].SourceBlock = nil, want non-nil")
	}
}

func TestAcceptanceErrorsBlockEntrySpan(t *testing.T) {
	src := `errors /* ->
  NotFound notFoundJSON

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	em := r.Handlers[0].ErrorMap
	if len(em) == 0 {
		t.Fatalf("ErrorMap is empty")
	}
	if em[0].TypeSpan.Start.Line != 2 {
		t.Errorf("TypeSpan line = %d, want 2", em[0].TypeSpan.Start.Line)
	}
	if em[0].FormatterSpan.Start.Line != 2 {
		t.Errorf("FormatterSpan line = %d, want 2", em[0].FormatterSpan.Start.Line)
	}
}

// =====================================================================
// Ambiguous Errors Block Membership
// =====================================================================

func TestAcceptanceAmbiguousErrorsBlockReportsConflict(t *testing.T) {
	src := `errors /admin/* ->
  NotFound adminNotFoundJSON

errors /:tenant/users/* ->
  NotFound tenantNotFoundJSON

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, AmbiguousErrorsBlock) {
		t.Fatalf("expected AmbiguousErrorsBlock error, got %v", errs)
	}
	for _, e := range errs {
		if e.Kind != AmbiguousErrorsBlock {
			continue
		}
		if len(e.Spans) != 2 {
			t.Errorf("expected 2 conflicting spans, got %d", len(e.Spans))
		}
	}
}

func TestAcceptanceAmbiguousErrorsBlockKeptChainFallback(t *testing.T) {
	// Three errors blocks: /* (chain root), /admin/* (chain), and an
	// unrelated /:tenant/users/* that conflicts with /admin/*. The
	// kept set should be {/*, /admin/*}; the unrelated block conflicts.
	src := `errors /* ->
  ServerError serverErrJSON

errors /admin/* ->
  NotFound notFoundJSON

errors /:tenant/users/* ->
  NotFound tenantNotFoundJSON

GET /admin/users/:id ->
  format u.html with x
`
	r, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, AmbiguousErrorsBlock) {
		t.Fatalf("expected AmbiguousErrorsBlock error, got %v", errs)
	}
	em := r.Handlers[0].ErrorMap
	gotTypes := map[string]string{}
	for _, e := range em {
		gotTypes[e.TypeName] = e.Formatter
	}
	if gotTypes["ServerError"] != "serverErrJSON" {
		t.Errorf("ServerError formatter = %q, want serverErrJSON", gotTypes["ServerError"])
	}
	if gotTypes["NotFound"] != "notFoundJSON" {
		t.Errorf("NotFound formatter = %q, want notFoundJSON (from /admin/*, chain kept)",
			gotTypes["NotFound"])
	}
}

func TestAcceptanceAmbiguousErrorsBlockNoCleanChainEmptyMap(t *testing.T) {
	// Two errors blocks that overlap each other without containment
	// and no unconflicted alternative — the kept set is empty.
	src := `errors /admin/* ->
  NotFound adminJSON

errors /:tenant/users/* ->
  NotFound tenantJSON

GET /admin/users/:id ->
  format u.html with x
`
	r, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, AmbiguousErrorsBlock) {
		t.Fatalf("expected AmbiguousErrorsBlock error")
	}
	if len(r.Handlers[0].ErrorMap) != 0 {
		t.Errorf("ErrorMap = %v, want empty", r.Handlers[0].ErrorMap)
	}
}

// =====================================================================
// Canonical Stage Order
// =====================================================================

func TestAcceptanceCanonicalOrderHandlerNoError(t *testing.T) {
	src := `GET /users/:id ->
  session cookie
  csrf auto
  approve auth.isOwner
  resolve user = db.users(:id)
  commit logged = X.log()
  emit user.read
  layout app
  format u.html with user
`
	_, errs := elab(t, "p.writ", src)
	if hasErrorKind(errs, StageOrder) {
		t.Errorf("expected no StageOrder error, got %v", errs)
	}
}

func TestAcceptanceNonCanonicalHandlerReportsAtOffender(t *testing.T) {
	src := `GET /users/:id ->
  approve auth.isOwner
  csrf auto
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, StageOrder) {
		t.Fatalf("expected StageOrder error, got %v", errs)
	}
	for _, e := range errs {
		if e.Kind != StageOrder {
			continue
		}
		if e.Span.Start.Line != 3 {
			t.Errorf("StageOrder span line = %d, want 3 (the csrf line)",
				e.Span.Start.Line)
		}
	}
}

func TestAcceptanceNonCanonicalSystemReports(t *testing.T) {
	src := `system ->
  approve auth.X
  csrf auto

GET /users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, StageOrder) {
		t.Errorf("expected StageOrder error from system block")
	}
}

func TestAcceptanceNonCanonicalGroupReports(t *testing.T) {
	src := `group /admin/* ->
  approve auth.X
  csrf auto

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, StageOrder) {
		t.Errorf("expected StageOrder error from group block")
	}
}

func TestAcceptanceObservationalExemptFromCanonicalOrder(t *testing.T) {
	src := `GET /users/:id ->
  log :id
  approve auth.isOwner
  resolve user = db.users(:id)
  log :user.id
  measure :user.id
  commit logged = X.log()
  log :user.id
  format u.html with user
`
	_, errs := elab(t, "p.writ", src)
	if hasErrorKind(errs, StageOrder) {
		t.Errorf("observationals should be exempt from canonical-order check, got %v", errs)
	}
}

func TestAcceptanceStageOrderErrorCarriesFileLineColumn(t *testing.T) {
	src := `GET /users/:id ->
  approve auth.X
  csrf auto
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	for _, e := range errs {
		if e.Kind != StageOrder {
			continue
		}
		if e.File == "" {
			t.Errorf("StageOrder.File is empty")
		}
		if e.Line == 0 {
			t.Errorf("StageOrder.Line is 0")
		}
		if e.Column == 0 {
			t.Errorf("StageOrder.Column is 0")
		}
		if e.Message == "" {
			t.Errorf("StageOrder.Message is empty")
		}
	}
}

func TestAcceptanceStageOrderViolatorPlacedCanonically(t *testing.T) {
	// Even after a stage-order violation, the resolved entry's
	// effective stage list still contains the offending statement,
	// placed in its canonical position.
	src := `GET /users/:id ->
  approve auth.X
  csrf auto
  format u.html with x
`
	r, _ := elab(t, "p.writ", src)
	stages := r.Handlers[0].Stages
	wantKinds := []StageKind{StageCSRF, StageApprove, StageFormat}
	if got := stageKindsOf(stages); !equalKinds(got, wantKinds) {
		t.Errorf("stages = %v, want canonical order %v", got, wantKinds)
	}
}

// =====================================================================
// Source Provenance
// =====================================================================

func TestAcceptanceSourceProvenanceSpanAtWinningDeclaration(t *testing.T) {
	src := `system ->
  approve auth.X

GET /users/:id ->
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve")
	}
	if approve.Span().Start.Line != 2 {
		t.Errorf("span line = %d, want 2 (system declaration)", approve.Span().Start.Line)
	}
	if approve.Span().Start.Source.Path != "p.writ" {
		t.Errorf("span source = %q, want p.writ", approve.Span().Start.Source.Path)
	}
}

func TestAcceptanceSourceProvenanceAfterIncludeFlatten(t *testing.T) {
	// Includes can carry groups (system/handler blocks must live at
	// the root). After flattening, the group's spans still point at
	// the included file.
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`include groups.writ
GET /admin/users/:id ->
  format u.html with x
`)},
		"groups.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
`)},
	}
	prog, perrs := parser.Parse("app.writ", parser.WithFS(fsys))
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	r, errs := Elaborate(prog)
	if len(errs) != 0 {
		t.Fatalf("elaborate errors: %v", errs)
	}
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve")
	}
	if approve.Span().Start.Source.Path != "groups.writ" {
		t.Errorf("span source = %q, want groups.writ (originating file, not flattened root)",
			approve.Span().Start.Source.Path)
	}
}

// =====================================================================
// Empty / Non-Existent Stages
// =====================================================================

func TestAcceptanceUndeclaredStageHasNoEntry(t *testing.T) {
	src := `GET /users/:id ->
  log :id
  format u.html with x
`
	r := elabOK(t, "p.writ", src)
	if firstStageOfKind(r.Handlers[0].Stages, StageApprove) != nil {
		t.Errorf("undeclared approve should not appear in Stages")
	}
	if hasOptOut(r.Handlers[0].OptOuts, StageApprove) {
		t.Errorf("undeclared approve should not appear in OptOuts")
	}
}

func TestAcceptanceSystemBlockNoHandlersZeroEntries(t *testing.T) {
	src := `system ->
  approve auth.X
`
	r := elabOK(t, "p.writ", src)
	if len(r.Handlers) != 0 {
		t.Errorf("handlers = %d, want 0", len(r.Handlers))
	}
}

func TestAcceptancePartialHandlerSkippedSilently(t *testing.T) {
	// A parser-error AST containing both well-formed and partial
	// handler nodes — the partial one (missing route pattern) is
	// skipped without generating an additional elaboration error.
	src := `GET /good ->
  log :id
  format good.html with x
GET ->
  log :id
  format bad.html with x
GET /also-good ->
  log :id
  format ok.html with x
`
	prog, perrs := parser.ParseString("p.writ", src)
	if len(perrs) == 0 {
		t.Fatalf("expected parser errors for the partial handler")
	}
	resolved, errs := Elaborate(prog)
	if len(errs) != 0 {
		t.Errorf("elaborate should not generate errors for partial handlers, got %v", errs)
	}
	if len(resolved.Handlers) != 2 {
		t.Errorf("handlers = %d, want 2 (only well-formed)", len(resolved.Handlers))
	}
}

func TestAcceptancePartialSystemBlockTreatedAsAbsent(t *testing.T) {
	// Parser-error AST with a partial system block: handlers should
	// not inherit anything from it.
	src := `system ->
  approve@@@

group /admin/* ->
  approve auth.isAdmin

GET /admin/users/:id ->
  format u.html with x
`
	prog, perrs := parser.ParseString("p.writ", src)
	if len(perrs) == 0 {
		t.Fatalf("expected parser errors for the malformed system stmt")
	}
	resolved, _ := Elaborate(prog)
	if len(resolved.Handlers) != 1 {
		t.Fatalf("handlers = %d, want 1", len(resolved.Handlers))
	}
	approve := firstStageOfKind(resolved.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Fatalf("expected approve from group, got none")
	}
	if approve.SourceLevel() != SourceGroup {
		t.Errorf("approve.SourceLevel = %v, want SourceGroup (system was partial)",
			approve.SourceLevel())
	}
}

// =====================================================================
// Stage-Placement Errors
// =====================================================================

func TestAcceptanceFormatInSystemIsPlacementError(t *testing.T) {
	src := `system ->
  format global.html with x

GET /users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, StagePlacement) {
		t.Fatalf("expected StagePlacement error, got %v", errs)
	}
	for _, e := range errs {
		if e.Kind != StagePlacement {
			continue
		}
		if e.Span.Start.Line != 2 {
			t.Errorf("StagePlacement span line = %d, want 2 (the format line)",
				e.Span.Start.Line)
		}
	}
}

func TestAcceptanceFormatInGroupIsPlacementError(t *testing.T) {
	src := `group /admin/* ->
  format global.html with x

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, StagePlacement) {
		t.Errorf("expected StagePlacement error, got %v", errs)
	}
}

func TestAcceptanceRedirectInSystemOrGroupIsPlacementError(t *testing.T) {
	src := `system ->
  redirect /home

group /admin/* ->
  redirect /admin

GET /users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	count := countErrorKind(errs, StagePlacement)
	if count < 2 {
		t.Errorf("expected at least 2 StagePlacement errors (system + group), got %d in %v",
			count, errs)
	}
}

func TestAcceptanceFormatNoneAtAnyLevelIsPlacementError(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"system", `system ->
  format none

GET /users/:id ->
  format u.html with x
`},
		{"group", `group /admin/* ->
  format none

GET /admin/users/:id ->
  format u.html with x
`},
		{"handler", `GET /users/:id ->
  format none
  format u.html with x
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := elab(t, "p.writ", tc.src)
			if !hasErrorKind(errs, StagePlacement) {
				t.Errorf("expected StagePlacement error for format none at %s level, got %v",
					tc.name, errs)
			}
		})
	}
}

func TestAcceptanceRedirectNoneAtAnyLevelIsPlacementError(t *testing.T) {
	// The current parser doesn't accept `redirect none` token sequence.
	// Verify the elaborator's placement check at the AST level for
	// every level (system/group/handler).
	src := newSrc("p.writ")
	for _, level := range []SourceLevel{SourceSystem, SourceGroup, SourceHandler} {
		t.Run(level.String(), func(t *testing.T) {
			_, errs := composeLevel([]ast.Stmt{mkNone(src, 1, "redirect")}, level)
			if len(errs) == 0 {
				t.Fatalf("expected StagePlacement error, got none")
			}
			if errs[0].Kind != StagePlacement {
				t.Errorf("error kind = %v, want StagePlacement", errs[0].Kind)
			}
		})
	}
}

func TestAcceptancePlacementErrorCarriesFileLineColumnMessage(t *testing.T) {
	src := `system ->
  format bad.html with x

GET /users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	for _, e := range errs {
		if e.Kind != StagePlacement {
			continue
		}
		if e.File == "" || e.Line == 0 || e.Column == 0 || e.Message == "" {
			t.Errorf("StagePlacement missing required field: %+v", e)
		}
	}
}

func TestAcceptanceResolvedNonNilOnPlacementError(t *testing.T) {
	src := `system ->
  format bad.html with x

GET /users/:id ->
  format u.html with x
`
	r, errs := elab(t, "p.writ", src)
	if r == nil {
		t.Fatal("Resolved is nil on error; must always be non-nil")
		return
	}
	if len(errs) == 0 {
		t.Fatalf("expected errors")
	}
	if len(r.Handlers) != 1 {
		t.Errorf("expected 1 cleanly-resolved handler, got %d", len(r.Handlers))
	}
}

func TestAcceptanceMultipleViolationsInSinglePass(t *testing.T) {
	src := `system ->
  format sys.html with x
  redirect /sys

group /admin/* ->
  format grp.html with x

GET /admin/users/:id ->
  format none
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	count := countErrorKind(errs, StagePlacement)
	if count < 4 {
		t.Errorf("expected at least 4 StagePlacement errors in single pass, got %d in %v",
			count, errs)
	}
}

// =====================================================================
// Ambiguous Group Membership
// =====================================================================

func TestAcceptanceAmbiguousGroupReportsConflict(t *testing.T) {
	src := `group /admin/* ->
  approve auth.isAdmin

group /:tenant/users/* ->
  approve auth.tenantAccess

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, AmbiguousGroup) {
		t.Fatalf("expected AmbiguousGroup error, got %v", errs)
	}
}

func TestAcceptanceAmbiguousGroupErrorReferencesAllSpans(t *testing.T) {
	src := `group /admin/* ->
  approve auth.isAdmin

group /:tenant/users/* ->
  approve auth.tenantAccess

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	for _, e := range errs {
		if e.Kind != AmbiguousGroup {
			continue
		}
		if len(e.Spans) != 2 {
			t.Errorf("expected 2 conflicting group spans, got %d", len(e.Spans))
		}
		// Primary Span is the affected handler.
		if e.Span.Start.Line != 7 {
			t.Errorf("AmbiguousGroup primary span line = %d, want 7 (handler)",
				e.Span.Start.Line)
		}
	}
}

func TestAcceptanceAmbiguousGroupHandlerGetsSystemOnlyInheritance(t *testing.T) {
	src := `system ->
  approve auth.isUser

group /admin/* ->
  approve auth.isAdmin

group /:tenant/users/* ->
  approve auth.tenantAccess

GET /admin/users/:id ->
  format u.html with x
`
	r, _ := elab(t, "p.writ", src)
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Fatalf("missing approve")
	}
	if approve.SourceLevel() != SourceSystem {
		t.Errorf("approve.SourceLevel = %v, want SourceSystem (groups skipped)",
			approve.SourceLevel())
	}
}

func TestAcceptanceThreeGroupChainNoAmbiguity(t *testing.T) {
	src := `group /* ->
  csrf auto

group /admin/* ->
  approve auth.isAdmin

group /admin/users/* ->
  limit rate.adminUsers()

GET /admin/users/:id ->
  format u.html with x
`
	_, errs := elab(t, "p.writ", src)
	if hasErrorKind(errs, AmbiguousGroup) {
		t.Errorf("three-group containment chain should not be ambiguous, got %v", errs)
	}
}

func TestAcceptanceChainPlusUnrelatedReportsOnlyUnrelated(t *testing.T) {
	src := `group /* ->
  csrf auto

group /admin/* ->
  approve auth.isAdmin

group /:tenant/users/* ->
  limit rate.tenant()

GET /admin/users/:id ->
  format u.html with x
`
	r, errs := elab(t, "p.writ", src)
	if !hasErrorKind(errs, AmbiguousGroup) {
		t.Fatalf("expected AmbiguousGroup error for unrelated group")
	}
	for _, e := range errs {
		if e.Kind != AmbiguousGroup {
			continue
		}
		if len(e.Spans) != 1 {
			t.Errorf("expected 1 conflicting span (only the unrelated group), got %d in %v",
				len(e.Spans), e.Spans)
		}
	}
	// The chain should still layer: handler should inherit csrf
	// (from /*) and approve (from /admin/*).
	csrf := firstStageOfKind(r.Handlers[0].Stages, StageCSRF)
	if csrf == nil {
		t.Errorf("expected csrf inherited from /* chain root")
	}
	approve := firstStageOfKind(r.Handlers[0].Stages, StageApprove)
	if approve == nil {
		t.Errorf("expected approve inherited from /admin/*")
	}
}

// stagesOfKind returns every stage of kind in order.
func stagesOfKind(stages []Stage, kind StageKind) []Stage {
	var out []Stage
	for _, s := range stages {
		if s.Kind() == kind {
			out = append(out, s)
		}
	}
	return out
}
