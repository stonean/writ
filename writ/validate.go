package writ

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stonean/writ/ast"
	"github.com/stonean/writ/pipeline"
)

// validate runs the runtime's startup-validation passes against a
// resolved program plus the registration tables. It returns the
// compiled routing table and an ordered list of validation entries.
//
// The pass order is:
//
//  1. Pipeline-shape sanity: every handler must contain exactly one
//     format step. The pipeline elaborator already guarantees this
//     for the in-scope subset, but re-check defensively so a future
//     elaborator change cannot produce a routing table without a
//     formatter.
//  2. Per-stage compilation (via [compileRoutes]): unregistered
//     resolver/formatter names, unsupported stages, undeclared route
//     parameters, and out-of-scope argument shapes.
//  3. Route ambiguity: two handlers declared on the same
//     method-and-canonical-path.
//
// On a non-empty entries slice the caller should treat the load as
// failed; the returned table may be partial (handlers that compiled
// cleanly are present) so tests that want to exercise the table on
// failure can still do so.
func validate(
	resolved *pipeline.Resolved,
	resolvers map[string]ResolverFunc,
	formatters map[string]FormatterFunc,
) (*routingTable, []Entry) {
	var entries []Entry

	entries = append(entries, checkPipelineShape(resolved)...)
	table, compileEntries := compileRoutes(resolved, resolvers, formatters)
	entries = append(entries, compileEntries...)
	entries = append(entries, checkRouteAmbiguity(resolved)...)

	return table, entries
}

// checkPipelineShape emits an entry for every handler whose
// effective pipeline does not end in exactly one format step. Spec
// 003 supports `resolve` and `format` only; spec 002's elaborator
// already rejects pipelines without a terminator, but a future
// change to the elaborator could weaken that guarantee, so this
// pass is the runtime's defensive backstop.
func checkPipelineShape(resolved *pipeline.Resolved) []Entry {
	var entries []Entry
	for _, h := range resolved.Handlers {
		formatCount := 0
		for _, stage := range h.Stages {
			if stage.Kind() == pipeline.StageFormat {
				formatCount++
			}
		}
		if formatCount == 1 {
			continue
		}
		entries = append(entries, Entry{
			Kind:    KindUnsupportedStage,
			Message: fmt.Sprintf("handler must end in exactly one format step; found %d", formatCount),
			Span:    h.Span,
		})
	}
	return entries
}

// checkRouteAmbiguity emits a KindRouteAmbiguity entry for every
// (method, canonicalPath) pair that has more than one declared
// handler. The primary Span points at the first handler; the
// additional Spans list the remaining colliding handlers in
// declaration order so tooling can surface every site.
//
// Canonical paths replace parameter segment names with ":" so
// /users/:id and /users/:user_id collide as the same shape.
func checkRouteAmbiguity(resolved *pipeline.Resolved) []Entry {
	type key struct{ method, path string }
	groups := make(map[key][]*pipeline.Handler)
	var order []key

	for _, h := range resolved.Handlers {
		if h.Pattern == nil {
			continue
		}
		k := key{method: h.Method, path: canonicalPath(h.Pattern)}
		if _, exists := groups[k]; !exists {
			order = append(order, k)
		}
		groups[k] = append(groups[k], h)
	}

	var entries []Entry
	for _, k := range order {
		group := groups[k]
		if len(group) < 2 {
			continue
		}
		spans := make([]ast.Span, 0, len(group)-1)
		for _, h := range group[1:] {
			spans = append(spans, h.Span)
		}
		entries = append(entries, Entry{
			Kind:    KindRouteAmbiguity,
			Message: fmt.Sprintf("%s %s is declared by %d handlers", k.method, k.path, len(group)),
			Span:    group[0].Span,
			Spans:   spans,
		})
	}

	// Sort entries by primary span line for deterministic ordering
	// across runs. checkRouteAmbiguity is the only validation pass
	// whose output groups by composite key rather than walking
	// resolved.Handlers in source order, so explicit sorting is
	// necessary to keep load output deterministic.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Span.Start.Offset < entries[j].Span.Start.Offset
	})
	return entries
}

// canonicalPath returns the route pattern with parameter segment
// names replaced by ":" so /users/:id and /users/:user_id collide
// as the same route shape. The leading "/" is preserved; the empty
// pattern (root "/") returns "/".
func canonicalPath(pattern *ast.RoutePattern) string {
	if pattern == nil || len(pattern.Segments) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, seg := range pattern.Segments {
		b.WriteByte('/')
		switch s := seg.(type) {
		case *ast.LiteralSegment:
			b.WriteString(s.Text)
		case *ast.ParameterSegment:
			b.WriteByte(':')
		case *ast.WildcardSegment:
			b.WriteByte('*')
		}
	}
	return b.String()
}
