package pipeline

import (
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
)

// stub is a tiny test stand-in for *ast.GroupBlock or *ast.ErrorsBlock.
// It carries only the fields findKept and sortBySpecificity rely on.
type stub struct {
	pattern *ast.RoutePattern
	order   int
	label   string
}

func patternOfStub(s *stub) *ast.RoutePattern { return s.pattern }
func declOrderOfStub(s *stub) int             { return s.order }

func newStub(t *testing.T, label string, order int, pattern string) *stub {
	t.Helper()
	return &stub{pattern: pat(t, pattern), order: order, label: label}
}

func labels(stubs []*stub) []string {
	out := make([]string, len(stubs))
	for i, s := range stubs {
		out[i] = s.label
	}
	return out
}

func TestFindKeptNoMatches(t *testing.T) {
	candidates := []*stub{
		newStub(t, "users", 0, "/users/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/dashboard"))
	if len(kept) != 0 || len(conflicting) != 0 {
		t.Fatalf("kept=%v conflicting=%v, want both empty", labels(kept), labels(conflicting))
	}
}

func TestFindKeptSingleMatch(t *testing.T) {
	candidates := []*stub{
		newStub(t, "admin", 0, "/admin/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users"))
	if !reflect.DeepEqual(labels(kept), []string{"admin"}) {
		t.Fatalf("kept = %v, want [admin]", labels(kept))
	}
	if len(conflicting) != 0 {
		t.Fatalf("conflicting = %v, want []", labels(conflicting))
	}
}

func TestFindKeptTwoGroupContainmentChain(t *testing.T) {
	candidates := []*stub{
		newStub(t, "admin", 0, "/admin/*"),
		newStub(t, "admin-users", 1, "/admin/users/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users/1"))
	if !reflect.DeepEqual(labels(kept), []string{"admin", "admin-users"}) {
		t.Fatalf("kept = %v, want [admin admin-users]", labels(kept))
	}
	if len(conflicting) != 0 {
		t.Fatalf("conflicting = %v, want []", labels(conflicting))
	}
}

func TestFindKeptThreeGroupContainmentChain(t *testing.T) {
	candidates := []*stub{
		newStub(t, "root", 0, "/*"),
		newStub(t, "admin", 1, "/admin/*"),
		newStub(t, "admin-users", 2, "/admin/users/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users/1"))
	if !reflect.DeepEqual(labels(kept), []string{"root", "admin", "admin-users"}) {
		t.Fatalf("kept = %v", labels(kept))
	}
	if len(conflicting) != 0 {
		t.Fatalf("conflicting = %v, want []", labels(conflicting))
	}
}

func TestFindKeptOverlappingGroupsAreConflicting(t *testing.T) {
	// /admin/* and /:tenant/users/* both match /admin/users/1
	// Neither pattern contains the other, so both are conflicting.
	candidates := []*stub{
		newStub(t, "admin", 0, "/admin/*"),
		newStub(t, "tenant-users", 1, "/:tenant/users/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users/1"))
	if len(kept) != 0 {
		t.Fatalf("kept = %v, want []", labels(kept))
	}
	if !reflect.DeepEqual(labels(conflicting), []string{"admin", "tenant-users"}) {
		t.Fatalf("conflicting = %v, want [admin tenant-users]", labels(conflicting))
	}
}

func TestFindKeptChainPlusUnrelatedOverlap(t *testing.T) {
	// /admin/* ⊂ /* (chain).
	// /:tenant/users/* overlaps /admin/* without containment, and
	// overlaps /* (it's contained in /*).
	// Wait: /:tenant/users/* IS contained in /* (every 3+ segment path
	// is matched by /*). So /:tenant/users/* sits in containment with
	// /* but conflicts with /admin/*. Per the spec, only /:tenant/users/*
	// is reported as conflicting; the {/* , /admin/*} chain layers
	// normally.
	candidates := []*stub{
		newStub(t, "root", 0, "/*"),
		newStub(t, "admin", 1, "/admin/*"),
		newStub(t, "tenant-users", 2, "/:tenant/users/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users/1"))
	if !reflect.DeepEqual(labels(kept), []string{"root", "admin"}) {
		t.Fatalf("kept = %v, want [root admin]", labels(kept))
	}
	if !reflect.DeepEqual(labels(conflicting), []string{"tenant-users"}) {
		t.Fatalf("conflicting = %v, want [tenant-users]", labels(conflicting))
	}
}

func TestFindKeptEqualPatternsKeptAsChain(t *testing.T) {
	// Two groups with equal patterns are mutually contained; both end
	// up in the kept set. (Treating duplicate patterns as a separate
	// diagnostic is out of scope for this spec.)
	candidates := []*stub{
		newStub(t, "admin-a", 0, "/admin/*"),
		newStub(t, "admin-b", 1, "/admin/*"),
	}
	kept, conflicting := findKept(candidates, patternOfStub, pat(t, "/admin/users/1"))
	if !reflect.DeepEqual(labels(kept), []string{"admin-a", "admin-b"}) {
		t.Fatalf("kept = %v, want [admin-a admin-b]", labels(kept))
	}
	if len(conflicting) != 0 {
		t.Fatalf("conflicting = %v, want []", labels(conflicting))
	}
}

func TestSortBySpecificityChain(t *testing.T) {
	// Provide an unsorted chain; expect least-to-most-specific output.
	stubs := []*stub{
		newStub(t, "admin-users", 2, "/admin/users/*"),
		newStub(t, "root", 0, "/*"),
		newStub(t, "admin", 1, "/admin/*"),
	}
	sortBySpecificity(stubs, patternOfStub, declOrderOfStub)
	if !reflect.DeepEqual(labels(stubs), []string{"root", "admin", "admin-users"}) {
		t.Fatalf("sort = %v, want [root admin admin-users]", labels(stubs))
	}
}

func TestSortBySpecificityEqualPatternsByDeclOrder(t *testing.T) {
	stubs := []*stub{
		newStub(t, "admin-b", 1, "/admin/*"),
		newStub(t, "admin-a", 0, "/admin/*"),
	}
	sortBySpecificity(stubs, patternOfStub, declOrderOfStub)
	if !reflect.DeepEqual(labels(stubs), []string{"admin-a", "admin-b"}) {
		t.Fatalf("sort = %v, want [admin-a admin-b]", labels(stubs))
	}
}

func TestSortBySpecificityStableForUnrelated(t *testing.T) {
	// Note: sortBySpecificity's precondition is a containment chain.
	// This test confirms the stable-sort tiebreak still applies if
	// callers happen to pass equal-pattern siblings in a row.
	stubs := []*stub{
		newStub(t, "root", 0, "/*"),
		newStub(t, "admin-a", 1, "/admin/*"),
		newStub(t, "admin-b", 2, "/admin/*"),
		newStub(t, "admin-users", 3, "/admin/users/*"),
	}
	sortBySpecificity(stubs, patternOfStub, declOrderOfStub)
	if !reflect.DeepEqual(labels(stubs), []string{"root", "admin-a", "admin-b", "admin-users"}) {
		t.Fatalf("sort = %v", labels(stubs))
	}
}
