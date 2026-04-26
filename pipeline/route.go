package pipeline

import "github.com/stonean/writ/ast"

// containsAll reports whether every route matched by sub is also
// matched by sup.
//
// A trailing wildcard in a pattern requires at least one additional
// segment (per spec 002 *Group Membership*: "`group /admin/*` matches
// every handler whose route starts with `/admin/`"). The four cases
// (sub × sup, with/without trailing wildcard):
//
//   - no wildcard ⊆ no wildcard: same length, segment-by-segment ⊆.
//   - no wildcard ⊆ wildcard prefix N: len(sub) > N, sub[i] ⊆ sup[i] for i < N.
//   - wildcard ⊆ no wildcard: never (sub matches arbitrarily long paths).
//   - wildcard prefix M ⊆ wildcard prefix N: M >= N, sub[i] ⊆ sup[i] for i < N.
func containsAll(sub, sup *ast.RoutePattern) bool {
	if sub == nil || sup == nil {
		return false
	}
	subSegs, subWild := splitSegments(sub)
	supSegs, supWild := splitSegments(sup)

	switch {
	case !supWild && !subWild:
		if len(subSegs) != len(supSegs) {
			return false
		}
	case !supWild && subWild:
		return false
	case supWild && !subWild:
		if len(subSegs) <= len(supSegs) {
			return false
		}
	case supWild && subWild:
		if len(subSegs) < len(supSegs) {
			return false
		}
	}

	for i, supSeg := range supSegs {
		if !segContains(subSegs[i], supSeg) {
			return false
		}
	}
	return true
}

// strictlyContains reports whether sub is strictly more specific than
// sup — every route matched by sub is matched by sup, but not vice
// versa.
func strictlyContains(sub, sup *ast.RoutePattern) bool {
	return containsAll(sub, sup) && !containsAll(sup, sub)
}

// equalPatterns reports whether two patterns match the same set of
// routes. True for syntactically identical patterns and for patterns
// differing only by parameter names.
func equalPatterns(a, b *ast.RoutePattern) bool {
	return containsAll(a, b) && containsAll(b, a)
}

// splitSegments returns the non-wildcard prefix of a pattern's
// segments and a flag indicating whether the pattern ends in a
// wildcard segment.
func splitSegments(p *ast.RoutePattern) (segs []ast.RouteSegment, wildcard bool) {
	if p == nil || len(p.Segments) == 0 {
		return nil, false
	}
	last := p.Segments[len(p.Segments)-1]
	if _, ok := last.(*ast.WildcardSegment); ok {
		return p.Segments[:len(p.Segments)-1], true
	}
	return p.Segments, false
}

// segContains reports whether every value matched by sub (at one
// segment position) is also matched by sup. Wildcard segments are
// peeled off by splitSegments and must not appear here.
func segContains(sub, sup ast.RouteSegment) bool {
	switch s := sup.(type) {
	case *ast.LiteralSegment:
		l, ok := sub.(*ast.LiteralSegment)
		return ok && l.Text == s.Text
	case *ast.ParameterSegment:
		switch sub.(type) {
		case *ast.LiteralSegment, *ast.ParameterSegment:
			return true
		}
		return false
	}
	return false
}
