package pipeline

import (
	"strings"
	"testing"

	"github.com/stonean/writ/ast"
)

// pat builds a *ast.RoutePattern from a string like "/users/:id/*".
// "/" produces an empty segments slice. Segments are split on "/".
// Each segment becomes a literal, a parameter (":name"), or a
// wildcard ("*"). Spans on the synthetic nodes are zero values; tests
// only inspect structural shape.
func pat(t *testing.T, s string) *ast.RoutePattern {
	t.Helper()
	if s == "" || s[0] != '/' {
		t.Fatalf("pattern must start with /, got %q", s)
	}
	p := ast.NewRoutePattern(ast.Span{})
	if s == "/" {
		return p
	}
	for part := range strings.SplitSeq(s[1:], "/") {
		var seg ast.RouteSegment
		switch {
		case part == "*":
			seg = ast.NewWildcardSegment(ast.Span{})
		case len(part) > 0 && part[0] == ':':
			seg = ast.NewParameterSegment(ast.Span{}, part[1:])
		default:
			seg = ast.NewLiteralSegment(ast.Span{}, part)
		}
		p.Segments = append(p.Segments, seg)
	}
	return p
}

func TestRouteContainsAll(t *testing.T) {
	cases := []struct {
		name string
		sub  string
		sup  string
		want bool
	}{
		// no-wildcard ⊆ no-wildcard
		{"identical literal", "/users/1", "/users/1", true},
		{"different literal segment", "/users/1", "/users/2", false},
		{"different length", "/users", "/users/1", false},
		{"literal sub ⊆ parameter sup", "/users/1", "/users/:id", true},
		{"parameter sub ⊆ parameter sup (different name)", "/users/:id", "/users/:user_id", true},
		{"parameter sub ⊄ literal sup", "/users/:id", "/users/1", false},
		{"identical parameters", "/users/:id", "/users/:id", true},

		// no-wildcard ⊆ wildcard
		{"three-segment ⊆ /admin/*", "/admin/users/1", "/admin/*", true},
		{"non-admin ⊄ /admin/*", "/users/1", "/admin/*", false},
		{"shorter than wildcard prefix", "/admin", "/admin/users/*", false},
		{"equal length to wildcard prefix needs strictly more", "/admin/users", "/admin/users/*", false},
		{"one segment past wildcard prefix", "/admin/users/1", "/admin/users/*", true},
		{"parameter at wildcard prefix", "/admin/:id", "/admin/*", true},

		// wildcard ⊆ no-wildcard (always false)
		{"wildcard ⊄ no-wildcard same prefix", "/admin/*", "/admin/users", false},
		{"wildcard ⊄ no-wildcard root", "/*", "/", false},

		// wildcard ⊆ wildcard
		{"longer wildcard ⊆ shorter", "/admin/users/*", "/admin/*", true},
		{"shorter wildcard ⊄ longer", "/admin/*", "/admin/users/*", false},
		{"unrelated wildcards", "/users/*", "/admin/*", false},
		{"identical wildcards", "/admin/*", "/admin/*", true},
		{"root wildcard contains everything", "/admin/*", "/*", true},

		// root pattern
		{"root ⊆ root", "/", "/", true},
		{"root ⊆ /*", "/", "/*", false},

		// nil safety
		{"empty patterns equal at structural level", "/users", "/users", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := containsAll(pat(t, c.sub), pat(t, c.sup))
			if got != c.want {
				t.Errorf("containsAll(%q, %q) = %v, want %v", c.sub, c.sup, got, c.want)
			}
		})
	}
}

func TestRouteContainsAllNil(t *testing.T) {
	p := pat(t, "/users")
	if containsAll(nil, p) {
		t.Error("containsAll(nil, p) should be false")
	}
	if containsAll(p, nil) {
		t.Error("containsAll(p, nil) should be false")
	}
	if containsAll(nil, nil) {
		t.Error("containsAll(nil, nil) should be false")
	}
}

func TestRouteStrictlyContains(t *testing.T) {
	cases := []struct {
		name string
		sub  string
		sup  string
		want bool
	}{
		{"strict subset literal vs parameter", "/users/1", "/users/:id", true},
		{"strict subset wildcard prefix", "/admin/users/*", "/admin/*", true},
		{"equal patterns are not strictly contained", "/users/:id", "/users/:user_id", false},
		{"unrelated patterns", "/users", "/admin", false},
		{"same direction only", "/users/:id", "/users/1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strictlyContains(pat(t, c.sub), pat(t, c.sup))
			if got != c.want {
				t.Errorf("strictlyContains(%q, %q) = %v, want %v", c.sub, c.sup, got, c.want)
			}
		})
	}
}

func TestRouteEqualPatterns(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"identical literals", "/users/1", "/users/1", true},
		{"identical parameters", "/users/:id", "/users/:id", true},
		{"parameter name difference", "/users/:id", "/users/:user_id", true},
		{"wildcard same prefix", "/admin/*", "/admin/*", true},
		{"different lengths", "/users", "/users/1", false},
		{"wildcard vs no wildcard", "/admin/*", "/admin", false},
		{"different literals", "/users", "/admin", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := equalPatterns(pat(t, c.a), pat(t, c.b))
			if got != c.want {
				t.Errorf("equalPatterns(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
