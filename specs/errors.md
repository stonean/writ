# Errors

Writ's error handling is built around three pieces:

1. **Typed Go errors** that carry their own HTTP status via a `StatusCode()` method.
2. **`errors /pattern ->` blocks** in the DSL that map error types to formatters, scoped by route prefix.
3. **The pipeline's `format` machinery**, reused to render the error response.

This document captures the conventions every error must follow. Specific error types belong to the feature that defines them.

## Typed Errors

Errors are ordinary Go types. A type carries its HTTP status by implementing `StatusCode() int`:

```go
type NotFound struct{ Resource string }
func (e NotFound) StatusCode() int { return 404 }

type Validation struct{ Fields map[string]string }
func (e Validation) StatusCode() int { return 422 }

type DuplicateEmail struct{ Email string }
func (e DuplicateEmail) StatusCode() int { return 409 }

type Unauthorized struct{ Message string }
func (e Unauthorized) StatusCode() int { return 401 }
```

If a returned error does not implement `StatusCode()`, the pipeline defaults the response to `500`.

## Status Mapping

Outside of typed errors, the pipeline assigns status codes from context:

| Outcome | Status |
| --- | --- |
| Successful resolve + format | 200 |
| Successful commit + format | 201 |
| Successful delete commit | 204 |
| Redirect after commit | 303 See Other |
| Redirect without commit | 302 Found |
| Approve failure | 401 or 403 |
| Limit failure | 429 |
| CSRF failure | 403 |
| Validation failure | from the error's `StatusCode()` (typically 422) |
| Other typed error | from the error's `StatusCode()` |
| Untyped error | 500 |

A formatter can override the success code with `writ.SetStatus(ctx, code)`.

## Errors Block

The DSL `errors` block scopes type-to-formatter mappings by route pattern:

```text
errors /* ->
  default error.json

errors /users/* ->
  DuplicateEmail  conflict.json
  NotFound        not_found.json
  Validation      validation.json
  default         error.json

errors /admin/* ->
  Forbidden       forbidden.html
  default         error.html
```

Rules:

- More specific route patterns win.
- A system-level block provides defaults; a group/handler scope overrides it.
- `default` is the catch-all for any error type not explicitly listed.
- Each entry maps a Go error type name (left) to a formatter name (right). The formatter is the same kind of formatter used by the `format` step.

## Validation Errors

The pipeline's input parser raises a `Validation` error when a body or query struct fails its `validate` tags. The error carries per-field details so a formatter can render structured per-field messages. The exact response shape is the formatter's responsibility — the pipeline does not impose a JSON envelope.

## Internal vs External

Storage-specific error codes, stack traces, and internal identifiers must not leak into the response body. Translate to a typed error at the boundary (a custom resolver, commit, or source adapter) and let the formatter produce a safe representation. Internal context (originals, stack, request ID) belongs in logs.

## Logging

Pipeline errors are logged at the `log` stage with structured fields including the error type, request ID, and originating stage. Severity convention:

| Severity | When |
| --- | --- |
| `warn` | Client errors (4xx) — expected, no action needed |
| `error` | Server errors (5xx) — unexpected, requires investigation |

## Error Catalog

<!-- Registry of error types in use across the project. Add entries as features
     introduce new typed errors. Example:

### NotFound

- **Status**: 404
- **Owner**: data layer (translated from source `not found`)
- **Carries**: `Resource` (string)

-->

_No project-wide errors registered yet. Per-feature error types live in their owning spec._
