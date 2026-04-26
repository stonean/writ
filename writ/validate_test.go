package writ

import (
	"strings"
	"testing"
)

func TestValidateUnregisteredResolver(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	if len(entries) != 1 || entries[0].Kind != KindUnregisteredResolver {
		t.Fatalf("entries = %v, want one KindUnregisteredResolver", entries)
	}
	if !strings.Contains(entries[0].Message, "db.users") {
		t.Errorf("message %q does not name the missing resolver", entries[0].Message)
	}
}

func TestValidateUnregisteredFormatter(t *testing.T) {
	src := `GET /users/:id ->
  format user.show with user
`
	resolved := mustElaborate(t, src)

	_, entries := validate(resolved, nil, nil)
	if len(entries) != 1 || entries[0].Kind != KindUnregisteredFormatter {
		t.Fatalf("entries = %v, want one KindUnregisteredFormatter", entries)
	}
	if !strings.Contains(entries[0].Message, "user.show") {
		t.Errorf("message %q does not name the missing formatter", entries[0].Message)
	}
}

func TestValidateUndeclaredRouteParameter(t *testing.T) {
	src := `GET /users/:id ->
  resolve other = db.other(:bogus)
  format user.show with other
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.other": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, resolvers, formatters)
	if len(entries) != 1 || entries[0].Kind != KindUndeclaredRouteParameter {
		t.Fatalf("entries = %v, want one KindUndeclaredRouteParameter", entries)
	}
	if !strings.Contains(entries[0].Message, "bogus") {
		t.Errorf("message %q does not name the missing parameter", entries[0].Message)
	}
}

func TestValidateUnsupportedStageReportedByPipelineShapeCheck(t *testing.T) {
	// `approve` is out of scope per spec 003. compileRoutes flags it
	// with KindUnsupportedStage; the validate orchestrator surfaces
	// the same kind. (The pipeline-shape check is independent —
	// without a format step it would also fire, but here the
	// handler still has a format step so only the per-stage
	// unsupported-stage entry is expected.)
	src := `GET /users/:id ->
  approve auth.isOwner
  format user.show with x
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnsupportedStage && strings.Contains(e.Message, "approve") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("entries = %v, want a KindUnsupportedStage mentioning approve", entries)
	}
}

func TestValidateRouteAmbiguity(t *testing.T) {
	// Two handlers on /users/:id under different parameter names
	// (the canonical path collapses :id and :user_id to ":") share
	// the same canonical shape and produce a KindRouteAmbiguity.
	src := `GET /users/:id ->
  format users.show with x

GET /users/:user_id ->
  format users.show with x
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"users.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	found := false
	for _, e := range entries {
		if e.Kind == KindRouteAmbiguity {
			if len(e.Spans) != 1 {
				t.Errorf("ambiguity entry expected 1 conflicting span, got %d", len(e.Spans))
			}
			if !strings.Contains(e.Message, "GET") || !strings.Contains(e.Message, "/users/:") {
				t.Errorf("message %q does not name the colliding method+path", e.Message)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want a KindRouteAmbiguity", entries)
	}
}

func TestValidateRouteAmbiguityThreeWayCollision(t *testing.T) {
	src := `GET /users/:id ->
  format users.show with x

GET /users/:user_id ->
  format users.show with x

GET /users/:slug ->
  format users.show with x
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"users.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	for _, e := range entries {
		if e.Kind != KindRouteAmbiguity {
			continue
		}
		if len(e.Spans) != 2 {
			t.Errorf("three-way collision should list 2 additional spans, got %d", len(e.Spans))
		}
	}
}

func TestValidateRouteAmbiguityIgnoresDifferentMethods(t *testing.T) {
	src := `GET /users/:id ->
  format users.show with x

POST /users/:id ->
  format users.show with x
`
	resolved := mustElaborate(t, src)
	formatters := map[string]FormatterFunc{"users.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	for _, e := range entries {
		if e.Kind == KindRouteAmbiguity {
			t.Errorf("different methods should not collide, got %v", e)
		}
	}
}

func TestValidatePipelineShapeBackstopCatchesNoFormat(t *testing.T) {
	// Construct a *pipeline.Resolved with a handler that has zero
	// format stages. The elaborator would normally reject this, so
	// we skip parsing and build the input directly.
	resolved := mustElaborate(t, `GET /users/:id ->
  format users.show with x
`)
	// Strip the format stage to simulate a future elaborator
	// regression that lets this slip through.
	resolved.Handlers[0].Stages = nil

	formatters := map[string]FormatterFunc{"users.show": noopFormatter}

	_, entries := validate(resolved, nil, formatters)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnsupportedStage && strings.Contains(e.Message, "exactly one format") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want pipeline-shape backstop entry", entries)
	}
}

func TestCanonicalPath(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"GET /users ->\n  format users.list with x\n", "/users"},
		{"GET /users/:id ->\n  format users.show with x\n", "/users/:"},
		{"GET /users/:id/posts ->\n  format users.posts with x\n", "/users/:/posts"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			resolved := mustElaborate(t, tc.src)
			got := canonicalPath(resolved.Handlers[0].Pattern)
			if got != tc.want {
				t.Errorf("canonicalPath = %q, want %q", got, tc.want)
			}
		})
	}
}
