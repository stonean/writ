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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, nil, nil, nil)
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

	_, entries := validate(resolved, resolvers, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

	_, entries := validate(resolved, nil, formatters, nil, nil)
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

func TestValidateUnregisteredErrorFormatter(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorTypes := map[string]func(error) bool{"NotFound": func(error) bool { return true }}

	_, entries := validate(resolved, resolvers, formatters, nil, errorTypes)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorFormatter && strings.Contains(e.Message, "notFoundJSON") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want a KindUnregisteredErrorFormatter naming notFoundJSON", entries)
	}
}

func TestValidateUnregisteredErrorType(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorFormatters := map[string]ErrorFormatterFunc{"notFoundJSON": noopErrorFormatter}

	_, entries := validate(resolved, resolvers, formatters, errorFormatters, nil)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorType && strings.Contains(e.Message, "NotFound") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want a KindUnregisteredErrorType naming NotFound", entries)
	}
}

func TestValidateUnregisteredErrorBothMissingReportsBoth(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, resolvers, formatters, nil, nil)
	gotType, gotFmt := false, false
	for _, e := range entries {
		switch e.Kind {
		case KindUnregisteredErrorType:
			gotType = true
		case KindUnregisteredErrorFormatter:
			gotFmt = true
		}
	}
	if !gotType || !gotFmt {
		t.Fatalf("entries = %v, want both KindUnregisteredErrorType and KindUnregisteredErrorFormatter", entries)
	}
}

func TestValidateDefaultEntryFlagsMissingFormatter(t *testing.T) {
	src := `errors /users/* ->
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, resolvers, formatters, nil, nil)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorFormatter && strings.Contains(e.Message, "defaultJSON") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want KindUnregisteredErrorFormatter for the default formatter", entries)
	}
}

func TestValidateDefaultEntryExemptFromTypeCheck(t *testing.T) {
	src := `errors /users/* ->
  default defaultJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorFormatters := map[string]ErrorFormatterFunc{"defaultJSON": noopErrorFormatter}

	_, entries := validate(resolved, resolvers, formatters, errorFormatters, nil)
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorType {
			t.Errorf("default entry should be exempt from type registration check, got %v", e)
		}
	}
}

func TestValidateNoErrorsBlockProducesNoErrorEntries(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}

	_, entries := validate(resolved, resolvers, formatters, nil, nil)
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorFormatter || e.Kind == KindUnregisteredErrorType {
			t.Errorf("no errors block should produce no error-related entries, got %v", e)
		}
	}
}

func TestValidateSuccessFormatterDoesNotSatisfyErrorFormatterCheck(t *testing.T) {
	src := `errors /users/* ->
  default sameName

GET /users/:id ->
  resolve user = db.users(:id)
  format sameName with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"sameName": noopFormatter}

	// errorFormatters does NOT contain "sameName" — only the success
	// registry does. The error-formatter check must still fire.
	_, entries := validate(resolved, resolvers, formatters, nil, nil)
	found := false
	for _, e := range entries {
		if e.Kind == KindUnregisteredErrorFormatter && strings.Contains(e.Message, "sameName") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want KindUnregisteredErrorFormatter (success-formatter registration must not satisfy the error-formatter check)", entries)
	}
}

func TestValidateUnregisteredErrorFormatterCarriesFormatterSpan(t *testing.T) {
	// KindUnregisteredErrorFormatter must reference the originating
	// errors-block entry's formatter span. Spec acceptance:
	// "Source provenance — KindUnregisteredErrorFormatter carries
	// the originating errors entry's span."
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	resolved := mustElaborate(t, src)
	resolvers := map[string]ResolverFunc{"db.users": noopResolver}
	formatters := map[string]FormatterFunc{"user.show": noopFormatter}
	errorTypes := map[string]func(error) bool{"NotFound": func(error) bool { return true }}

	_, entries := validate(resolved, resolvers, formatters, nil, errorTypes)
	for _, e := range entries {
		if e.Kind != KindUnregisteredErrorFormatter {
			continue
		}
		if e.Span.Start.Source == nil {
			t.Fatalf("KindUnregisteredErrorFormatter span has no source: %+v", e.Span)
		}
		if e.Span.Start.Line == 0 {
			t.Fatalf("KindUnregisteredErrorFormatter span has zero line: %+v", e.Span)
		}
		return
	}
	t.Fatalf("entries = %v, want a KindUnregisteredErrorFormatter", entries)
}
