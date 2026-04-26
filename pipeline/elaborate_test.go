package pipeline

import (
	"testing"

	"github.com/stonean/writ/parser"
)

// TestElaborateNilProgram exercises the nil-program guard.
func TestElaborateNilProgram(t *testing.T) {
	resolved, errs := Elaborate(nil)
	if resolved == nil {
		t.Fatalf("Elaborate(nil) returned nil *Resolved")
	}
	if len(resolved.Handlers) != 0 {
		t.Errorf("handlers = %d, want 0", len(resolved.Handlers))
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want empty", errs)
	}
}

// TestElaborateEmptyProgram exercises an empty source string: a non-nil
// *ast.Program with no system, no groups, no errors blocks, no
// handlers.
func TestElaborateEmptyProgram(t *testing.T) {
	prog, perrs := parser.ParseString("empty.writ", "")
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	resolved, errs := Elaborate(prog)
	if resolved == nil {
		t.Fatalf("Elaborate returned nil")
	}
	if len(resolved.Handlers) != 0 {
		t.Errorf("handlers = %d, want 0", len(resolved.Handlers))
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want empty", errs)
	}
}

// TestElaborateSystemOnlyProgram exercises a program with a system
// block but no handlers — the resolved value must have zero handler
// entries (not an error).
func TestElaborateSystemOnlyProgram(t *testing.T) {
	src := `system ->
  approve auth.isUser
`
	prog, perrs := parser.ParseString("p.writ", src)
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	resolved, errs := Elaborate(prog)
	if resolved == nil {
		t.Fatalf("Elaborate returned nil")
	}
	if len(resolved.Handlers) != 0 {
		t.Errorf("handlers = %d, want 0", len(resolved.Handlers))
	}
	if len(errs) != 0 {
		t.Errorf("errs = %v, want empty", errs)
	}
}

// TestElaborateSmokeRoundTrip is the task-8 smoke test: round-trip a
// small .writ source through parser.ParseString then Elaborate and
// assert handler count, basic stage shape, and zero errors.
func TestElaborateSmokeRoundTrip(t *testing.T) {
	src := `system ->
  approve auth.isUser
  resolve current_user = auth.user()

group /admin/* ->
  approve auth.isAdmin

errors /* ->
  NotFound notFoundJSON
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show.html with user

GET /admin/dashboard ->
  resolve stats = db.stats()
  format admin.dashboard.html with stats
`
	prog, perrs := parser.ParseString("p.writ", src)
	if len(perrs) != 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	resolved, errs := Elaborate(prog)
	if len(errs) != 0 {
		t.Fatalf("elaborate errors: %v", errs)
	}
	if resolved == nil {
		t.Fatalf("Elaborate returned nil")
	}
	if len(resolved.Handlers) != 2 {
		t.Fatalf("handlers = %d, want 2", len(resolved.Handlers))
	}

	users := resolved.Handlers[0]
	if users.Method != "GET" {
		t.Errorf("handler[0].Method = %q, want GET", users.Method)
	}
	wantUsers := []StageKind{StageApprove, StageResolve, StageResolve, StageFormat}
	if got := stageKindsOf(users.Stages); !equalKinds(got, wantUsers) {
		t.Errorf("handler[0] stages = %v, want %v", got, wantUsers)
	}
	wantUsersLevels := []SourceLevel{SourceSystem, SourceSystem, SourceHandler, SourceHandler}
	gotLevels := make([]SourceLevel, len(users.Stages))
	for i, s := range users.Stages {
		gotLevels[i] = s.SourceLevel()
	}
	if !equalLevels(gotLevels, wantUsersLevels) {
		t.Errorf("handler[0] levels = %v, want %v", gotLevels, wantUsersLevels)
	}
	if len(users.ErrorMap) != 2 {
		t.Errorf("handler[0].ErrorMap length = %d, want 2", len(users.ErrorMap))
	}

	admin := resolved.Handlers[1]
	if admin.Method != "GET" {
		t.Errorf("handler[1].Method = %q, want GET", admin.Method)
	}
	wantAdmin := []StageKind{StageApprove, StageResolve, StageResolve, StageFormat}
	if got := stageKindsOf(admin.Stages); !equalKinds(got, wantAdmin) {
		t.Errorf("handler[1] stages = %v, want %v", got, wantAdmin)
	}
	if got := admin.Stages[0].SourceLevel(); got != SourceGroup {
		t.Errorf("handler[1] approve level = %v, want SourceGroup", got)
	}
	if g := admin.Stages[0].SourceGroup(); g == nil {
		t.Errorf("handler[1] approve SourceGroup = nil, want non-nil")
	}
}

func equalKinds(a, b []StageKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalLevels(a, b []SourceLevel) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
