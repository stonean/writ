# 004 — Errors Block Runtime Data Model

The errors-block runtime introduces no database tables. Its data model is the new public Go surface plus the internal compiled-error structures the dispatcher walks per request. Both build on spec 003's data model; types not mentioned here are unchanged.

All exported types live in package `github.com/stonean/writ`. The package is documented as exported but unstable pre-1.0, matching the AST and pipeline packages.

## Public Types

### `ErrorFormatterFunc`

```go
type ErrorFormatterFunc func(ctx context.Context, w http.ResponseWriter, data ErrorData) error
```

The Go signature for `errors` block formatter implementations. The runtime invokes a registered error formatter when a resolver returns a non-nil error and the handler's effective error map matches. The formatter is responsible for writing the response status, headers, and body.

Symmetric in shape with `FormatterFunc` from spec 003. The third argument is the `ErrorData` accessor instead of `Results`.

### `ErrorData`

```go
type ErrorData struct {
    err     error
    status  int
    request *http.Request
}

func (d ErrorData) Err() error             // the value the resolver returned
func (d ErrorData) Status() int            // status resolved per *Status Resolution* in spec.md
func (d ErrorData) Request() *http.Request // the originating request
```

- `Err()` returns the raw error value as returned by the resolver. May be a typed struct, a pointer to a struct, or a wrapped error; `errors.As` and `errors.Is` work on it.
- `Status()` returns the status the runtime resolved before invoking the formatter: the value of `StatusCode()` when the error implements it, otherwise 500.
- `Request()` returns the live `*http.Request` the dispatcher received from `net/http`. Formatters use it for path-aware logging, request-id headers, content-type sniffing, and similar concerns.

The dispatcher constructs a fresh `ErrorData` per error-handling pass; values are passed by value to the formatter.

Future iterations may add accessors (parsed body, partial resolver results, request-id, retry hints) without changing the type's signature.

### Top-level generic registration

```go
func ErrorType[T error](w *Writ, name string) error
```

Registers a Go error type under a DSL identifier. The function is top-level (not a method) because Go does not permit type parameters on methods. The asymmetry with `(*Writ).ErrorFormatter(...)` is a language constraint; it is documented in the `writ` package comment.

Implementation: stores a closure of the form `func(err error) bool { var t T; return errors.As(err, &t) }` in the `Writ` instance's `errorTypes` registry under `name`. At dispatch time, the runtime invokes this closure to determine whether a returned error matches the registered type. `errors.As` walks the `Unwrap` chain, but it is *not* pointer-vs-value uniform: `ErrorType[NotFound]` matches `NotFound{}` returns; `ErrorType[*NotFound]` matches `*NotFound{}` returns. Resolvers that mix forms require both registrations or a project convention.

## New Method on `Writ`

```go
func (w *Writ) ErrorFormatter(name string, fn ErrorFormatterFunc) error
```

Registers an error formatter under a DSL identifier. Same lifecycle rules as `Resolver` and `Formatter` from spec 003: returns an error if `name` is already registered; panics if called outside `stateInit`.

## New `ErrorKind` Constants

```go
const (
    // Spec 003 kinds — KindParseFailure ... KindMissingEnvVar — unchanged.
    KindUnregisteredErrorFormatter
    KindUnregisteredErrorType
)
```

Appended to the existing `iota` block in `error.go`. Append-only ordering preserves the integer values of spec-003 kinds.

`ErrorKind.String()` gains two new cases:

- `KindUnregisteredErrorFormatter` → `"unregistered-error-formatter"`
- `KindUnregisteredErrorType` → `"unregistered-error-type"`

## Modified `Writ` Struct

Two new map fields, parallel in shape to the spec-003 `resolvers` and `formatters` maps:

```go
type Writ struct {
    // existing spec-003 fields...
    errorFormatters map[string]ErrorFormatterFunc
    errorTypes      map[string]func(error) bool
}
```

Initialized to non-nil empty maps in `New()`. Read at request time; written only during `stateInit` registration. Lifecycle rules from spec 003 (CAS-protected state, panic on post-Load mutation) apply unchanged.

## Internal Types

These are unexported. Documented here for plan-review traceability; consumers should not depend on them.

### `compiledRoute.errorEntries`

`compiledRoute` from spec 003 gains one new field:

```go
type compiledRoute struct {
    // existing spec-003 fields...
    errorEntries []compiledErrorEntry
}
```

Populated during `compileHandler` by walking `pipeline.Handler.ErrorMap`. Order is preserved verbatim from the elaborator (spec 002 documents this as most-specific-first per the effective-error-map rule). Empty for handlers whose effective error map is empty (no `errors` block matched the route).

### `compiledErrorEntry`

```go
type compiledErrorEntry struct {
    typeName  string             // DSL identifier from the errors-block entry; "default" for the catch-all
    isDefault bool               // true when the entry's left-hand side is `default`
    matcher   func(error) bool   // closure stored under typeName in errorTypes; nil when isDefault is true
    formatter ErrorFormatterFunc // pre-resolved at compile time from errorFormatters
    span      ast.Span           // originating *ast.ErrorsEntry span for diagnostics
}
```

The dispatcher walks this slice on the resolver-error path:

- For each entry where `!isDefault`: invoke `matcher(err)`. First match wins; no further entries are checked.
- If no concrete-type entry matched: invoke the first entry where `isDefault == true`.
- If neither path produced a match, fall through to the *Status Resolution* matrix's "No errors-block match" rows (per Q3 of `spec.md`).

`matcher` is nil for the default entry; the dispatcher checks `isDefault` before calling. `formatter` is always non-nil because validation rejects unregistered formatter names at startup.

## Lifecycle State

The lifecycle state machine from spec 003 (`stateInit`, `stateLoading`, `stateLoaded`) is unchanged. The new registration calls (`ErrorFormatter`, `ErrorType[T]`) honor the same state checks; they panic if called outside `stateInit`. The new validation pass runs inside `Load`'s existing pipeline; on failure it adds entries to the existing aggregate `*Error` and rolls back to `stateInit` per the spec-003 rule.

## Notes

- **No ordering across error formatters.** The runtime walks `errorEntries` in declaration order from `pipeline.Handler.ErrorMap`. Two error blocks at the same specificity layer in DSL declaration order; spec 002 has already chosen the winners. The runtime does not re-sort.
- **Matcher closures are not comparable.** Two `Load` calls of the same source produce structurally equal `errorEntries` slices but pointer-distinct `matcher` closures (each `errors.As`-wrapping closure is a fresh function value). The determinism test asserts `(typeName, isDefault, span)` index-by-index; function pointers are excluded.
- **No persistence.** All runtime state lives on the `Writ` instance. Dropping the instance reclaims the registries, the routing table, and the per-route error-entry slices. The runtime owns no goroutines, no timers, and no open files outside the spec-003 `Load` reads.
- **Future-feature compatibility.** Adding new `ErrorKind` constants is additive (consumers switching on `Kind` get a default branch on unknown values). Adding fields to `compiledErrorEntry` is invisible to consumers since the type is unexported. Adding accessors to `ErrorData` is additive on the public surface — existing formatters compile unchanged.
