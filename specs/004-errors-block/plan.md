# 004 — Errors Block Runtime Plan

## Overview

The errors-block runtime is a strict upgrade to spec 003: it consumes the per-handler effective error map already produced by spec 002 (exposed as `pipeline.Handler.ErrorMap`) and replaces spec 003's "any resolver error → runtime-owned 500" path with a typed-dispatch pipeline. The spec-003 generic 500 remains the universal fallback when no errors-block entry matches and the error has no `StatusCode()` method.

The implementation lives entirely inside the `writ` package. No new package boundaries, no parser or elaborator changes (both already accept `errors` blocks and surface them on `Resolved.Handlers`). The runtime adds three pieces of public surface (`ErrorFormatterFunc`, `ErrorData`, `writ.ErrorType[T]`), one new public method (`(*Writ).ErrorFormatter`), and two new `ErrorKind` constants. Existing programs without `errors` blocks behave identically — the new validation passes walk zero entries, and the dispatcher's new code paths are skipped because the compiled error-entry slice is empty.

The runtime stays reflect-free in production code: type-name dispatch goes through `errors.As` from the standard library (which itself uses reflection internally — that's stdlib's problem, not Writ's). Generics on the top-level `writ.ErrorType[T]` registration capture the type information the closure-stored matcher needs.

## Technical Decisions

### Stay in `writ` package

All new code lives in the existing `writ/` package alongside the spec-003 implementation. Adding a sub-package (`writ/errors`) was considered and rejected: the new surface is small (one method, one top-level function, one accessor type, one func type), the existing `writ` package already imports `pipeline` and `ast`, and splitting would force a re-export shim or break the `import "github.com/stonean/writ"` contract from spec 003.

File structure:

- New files: `error_data.go` (the `ErrorData` accessor and `ErrorFormatterFunc`), `error_dispatch.go` (status resolution, plain-text fallback writer, error-path orchestration helper).
- Modified files: `writ.go` (two new map fields), `register.go` (`ErrorFormatter` method, `ErrorType[T]` top-level), `error.go` (two new `Kind` constants + `String()` cases), `route.go` (`compiledRoute.errorEntries` field + compile pass), `validate.go` (new validation pass), `dispatch.go` (error-path branch).

### `writ.ErrorType[T]` is a top-level function

Go does not permit type parameters on methods. To accept a Go type as a registration argument, the registration entry point has to be a free function:

```go
func ErrorType[T error](w *Writ, name string) error {
    return w.registerErrorType(name, func(err error) bool {
        var t T
        return errors.As(err, &t)
    })
}
```

`registerErrorType` is the unexported method that does the actual map insertion + state check; `ErrorType[T]` is sugar that constructs the closure-based matcher. Power users who later need custom matching (e.g., `errors.Is`-based sentinel matching when that future feature lands) get a separate registration call at that time.

The asymmetry (`(*Writ).ErrorFormatter(...)` is a method, `writ.ErrorType[T](w, ...)` is a free function) is documented as a known Go-language wart in the package comment.

### `errors.As` matching internals

The closure stored under each registered type name has the shape `func(err error) bool`. Per request, the dispatcher walks the compiled error-entry slice and invokes each closure on the returned error. The closure invokes `errors.As(err, &t)` where `t` is a fresh local `T` per call — this matters for thread safety since `errors.As` mutates the target, and per-call locals avoid sharing state across goroutines.

`errors.As` walks the `Unwrap` chain automatically, so wrapped errors (`fmt.Errorf("...: %w", err)`) match the registration of the wrapped concrete type. It is *not* pointer-vs-value uniform: `errors.As(err, &t)` where `t` is `NotFound` matches a returned `NotFound{}` but not a returned `*NotFound{}`, and vice versa. Resolvers that sometimes return the value and sometimes the pointer require both `ErrorType[NotFound]` and `ErrorType[*NotFound]` registrations (each under its own DSL name) or a project convention to always return one form. Documented as a known limitation in the README; the runtime exposes no auto-double-registration shortcut because the constitution forbids reflect-based type introspection in production code, and a generic auto-promote would panic when `T` is itself a pointer type.

### `ErrorData` accessor type

```go
type ErrorData struct {
    err     error
    status  int
    request *http.Request
}

func (d ErrorData) Err() error             { return d.err }
func (d ErrorData) Status() int            { return d.status }
func (d ErrorData) Request() *http.Request { return d.request }
```

Symmetric in shape with `Params` and `Results` from spec 003. Constructed once per error-handling pass; passed by value to the formatter. Future accessors (parsed body, partial resolver results, request-id) are additive method additions.

### `ErrorFormatterFunc` signature

```go
type ErrorFormatterFunc func(ctx context.Context, w http.ResponseWriter, data ErrorData) error
```

Mirrors `FormatterFunc` from spec 003 in shape. The third argument is the projection-style accessor instead of `Results`.

### Three independent registries

`Writ` gains two map fields, both `map[string]<func>` shaped to match `resolvers` and `formatters` from spec 003:

```go
type Writ struct {
    // existing fields...
    errorFormatters map[string]ErrorFormatterFunc
    errorTypes      map[string]func(error) bool
    // ...
}
```

Each registry is independent: the same string `errorJSON` can appear in `formatters`, `errorFormatters`, and `errorTypes` without collision. Within each registry, duplicate registrations return an error per the spec-003 rule.

### Status resolution

The runtime detects the `StatusCode() int` method shape via an anonymous interface assertion:

```go
func resolveStatus(err error) (status int, hasStatusCode bool) {
    if sc, ok := err.(interface{ StatusCode() int }); ok {
        return sc.StatusCode(), true
    }
    return http.StatusInternalServerError, false
}
```

The boolean return distinguishes "explicitly opted in to a status" from "defaulted to 500." This matters for the Q3 fallback matrix: the no-match-but-has-StatusCode path writes `<status> <reason>\n` text; the no-match-no-StatusCode path writes the spec-003 generic 500 body.

`StatusCode()` is unwrapping-aware via `errors.As`-style probing? No — the runtime checks the immediate concrete type only. If a developer wraps a typed error and wants the wrapped error's status to surface, they implement `StatusCode()` on their wrapper. This matches the simplest behavior and avoids the runtime making decisions about which level of an `Unwrap` chain "owns" the status.

### Plain-text fallback writer

```go
func writeStatusText(rw http.ResponseWriter, status int) {
    rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
    rw.WriteHeader(status)
    text := http.StatusText(status)
    if text == "" {
        text = "Error"
    }
    fmt.Fprintf(rw, "%d %s\n", status, text)
}
```

Used for the Q3 "no errors-block match, but `StatusCode()` present" path. Symmetric with `write500` from spec 003 (which is now `writeStatusText(rw, 500)` in the empty-or-no-StatusCode case — but kept as its own function to preserve the exact existing body `"500 Internal Server Error\n"`).

### Compiled error entries

`compiledRoute` from spec 003 gains one new field:

```go
type compiledRoute struct {
    // existing fields from spec 003...
    errorEntries []compiledErrorEntry
}

type compiledErrorEntry struct {
    typeName  string             // for diagnostics; the DSL identifier
    isDefault bool               // true for the `default` keyword entry
    matcher   func(error) bool   // nil when isDefault is true
    formatter ErrorFormatterFunc // pre-resolved at compile time
    span      ast.Span           // originating ErrorsEntry span
}
```

The slice is populated at `compileHandler` time by walking `handler.ErrorMap`. Order is preserved verbatim from `pipeline.Handler.ErrorMap`, which spec 002 documents as most-specific-first per the effective-error-map rule. Entries with `IsDefault == true` have a nil `matcher`; the dispatcher tests them only after no concrete-type entry has matched.

### Validation pass

`validate.go` gains a new pass that runs after the existing pipeline-shape and per-stage checks:

```go
func checkErrorMap(handler *pipeline.Handler, errorTypes, errorFormatters map[string]...) []Entry {
    var entries []Entry
    for _, e := range handler.ErrorMap {
        if !e.IsDefault {
            if _, ok := errorTypes[e.TypeName]; !ok {
                entries = append(entries, Entry{
                    Kind:    KindUnregisteredErrorType,
                    Message: fmt.Sprintf("error type %q is not registered", e.TypeName),
                    Span:    e.TypeSpan,
                })
            }
        }
        if _, ok := errorFormatters[e.Formatter]; !ok {
            entries = append(entries, Entry{
                Kind:    KindUnregisteredErrorFormatter,
                Message: fmt.Sprintf("error formatter %q is not registered", e.Formatter),
                Span:    e.FormatterSpan,
            })
        }
    }
    return entries
}
```

The orchestrator (`validate`) calls this once per handler and threads the entries into the existing aggregate.

### Dispatch path

The error-handling section of `ServeHTTP` becomes:

```go
for _, step := range route.resolves {
    val, err := step.fn(ctx, paramsForCall(params, step.paramArgs))
    if err != nil {
        handleResolverError(rw, req, route, err)
        return
    }
    results[step.name] = val
}
```

Where `handleResolverError` is the new helper:

```go
func handleResolverError(rw http.ResponseWriter, req *http.Request, route *compiledRoute, err error) {
    status, hasStatusCode := resolveStatus(err)

    // 1. Try concrete-type entries.
    var defaultEntry *compiledErrorEntry
    for i := range route.errorEntries {
        e := &route.errorEntries[i]
        if e.isDefault {
            if defaultEntry == nil {
                defaultEntry = e
            }
            continue
        }
        if e.matcher(err) {
            invokeErrorFormatter(rw, req, *e, err, status)
            return
        }
    }

    // 2. Try the default entry.
    if defaultEntry != nil {
        invokeErrorFormatter(rw, req, *defaultEntry, err, status)
        return
    }

    // 3. No errors-block match: Q3 fallback.
    if hasStatusCode {
        writeStatusText(rw, status)
        return
    }
    write500(rw)
}
```

`invokeErrorFormatter` wraps `rw` with a `writeRecorder` (the same one spec 003 uses) and applies the same pre-/post-write rules as the success-path formatter:

```go
func invokeErrorFormatter(rw http.ResponseWriter, req *http.Request, entry compiledErrorEntry, err error, status int) {
    recorder := &writeRecorder{ResponseWriter: rw}
    data := ErrorData{err: err, status: status, request: req}
    if formatErr := entry.formatter(context.Background(), recorder, data); formatErr != nil {
        if !recorder.hasWritten {
            write500(rw)
        }
    }
}
```

Wait — `context.Background()` is wrong; we need `req.Context()`. The dispatcher passes the live request context through.

### `KindUnregisteredErrorFormatter` and `KindUnregisteredErrorType`

Appended to the existing `ErrorKind` iota block in `error.go`:

```go
const (
    KindParseFailure ErrorKind = iota
    // ... existing kinds ...
    KindMissingEnvVar
    KindUnregisteredErrorFormatter
    KindUnregisteredErrorType
)
```

Append-only ordering matters: existing tests and consumers rely on `iota` values not shifting. `String()` gets two new cases; everything else is unchanged.

### Determinism

Two `Load` calls of the same source must produce structurally equal compiled error-entry slices. The matcher closures are pointer-distinct across loads (each `errors.As`-wrapping closure is a fresh function value), so the determinism test compares by name and type-tag, not function identity:

```go
for i := range a.errorEntries {
    assert.Equal(a.errorEntries[i].typeName, b.errorEntries[i].typeName)
    assert.Equal(a.errorEntries[i].isDefault, b.errorEntries[i].isDefault)
    assert.Equal(a.errorEntries[i].span, b.errorEntries[i].span)
    // formatter and matcher are function pointers, not compared
}
```

### Backward compatibility

Programs loaded under spec 003 that declare no `errors` block now produce:

- `handler.ErrorMap == nil` (spec 002 already produces this for handlers with no matching errors block).
- `compiledRoute.errorEntries == nil` (the compile pass walks zero entries).
- Validation passes without entries.
- Dispatch's resolver-error branch falls through both the concrete-type loop and the default-entry check, hitting the Q3 fallback. Without `StatusCode()`, that's the spec-003 `write500` — byte-identical to spec 003's behavior.

Programs that *did* register `ErrorFormatter` or `ErrorType[T]` but declared no `errors` block elaborate cleanly and serve identically to spec-003 programs; the registrations are unused but accepted (per the silent-accept rule confirmed in Q7).

## Affected Files

| File | Action | Purpose |
| --- | --- | --- |
| `writ/writ.go` | Modify | Add `errorFormatters` and `errorTypes` map fields to the `Writ` struct; initialize in `New()` |
| `writ/register.go` | Modify | Add `(*Writ).ErrorFormatter(name, fn) error`; add unexported `(*Writ).registerErrorType(name, matcher) error`; add `mustBeInit` cases (or reuse existing) |
| `writ/error_data.go` | Create | `ErrorFormatterFunc` type, `ErrorData` accessor type with `Err`/`Status`/`Request` |
| `writ/error_register.go` | Create | Top-level `ErrorType[T error](w *Writ, name string) error` generic function |
| `writ/error.go` | Modify | Add `KindUnregisteredErrorFormatter` and `KindUnregisteredErrorType`; extend `ErrorKind.String()` |
| `writ/error_dispatch.go` | Create | `resolveStatus`, `writeStatusText`, `handleResolverError`, `invokeErrorFormatter` |
| `writ/route.go` | Modify | Add `errorEntries []compiledErrorEntry` and `compiledErrorEntry` to `compiledRoute`; extend `compileHandler` to populate the slice |
| `writ/validate.go` | Modify | Add `checkErrorMap` pass; call from `validate` orchestrator |
| `writ/dispatch.go` | Modify | Replace `write500(rw)` after a resolver error with `handleResolverError(rw, req, route, err)` |
| `writ/error_data_test.go` | Create | `ErrorData` accessor tests |
| `writ/error_register_test.go` | Create | `ErrorFormatter` and `ErrorType[T]` registration tests (uniqueness, post-Load panic, three-namespace independence) |
| `writ/error_dispatch_test.go` | Create | `resolveStatus`, `writeStatusText`, `handleResolverError` unit tests |
| `writ/validate_test.go` | Modify | Add `KindUnregisteredErrorFormatter` and `KindUnregisteredErrorType` cases |
| `writ/dispatch_test.go` | Modify | Add error-path scenarios: type match, default match, no-match with/without `StatusCode`, formatter pre/post-write error |
| `writ/acceptance_test.go` | Modify | Add the new acceptance criteria from spec 004 |
| `writ/determinism_test.go` | Modify | Extend determinism assertions to cover the per-handler `errorEntries` slice |
| `writ/testdata/errors_smoke.writ` | Create | Smoke fixture with multiple `errors` blocks, mix of typed and `default` entries |
| `writ/smoke_test.go` | Modify | Add a smoke test that exercises the new fixture end-to-end |
| `README.md` | Modify | Update feature 004 status to `planned` (later `done`) |

The companion `data-model.md` enumerates the new types, registries, and validation kinds.

## Trade-offs

### Considered and rejected

- **Internal `(*Writ).ErrorMatcher(name, fn func(error) bool)` primitive plus generic sugar.** Would let power users register custom matchers (e.g., sentinel matching via `errors.Is`) without waiting for a future feature. Rejected for now because the spec settles on `writ.ErrorType[T]` as the entry point and adding `ErrorMatcher` would be additional public surface ahead of any real consumer. When sentinel support lands as a future feature, the primitive can be added then.
- **Reflect-based dispatch (`reflect.TypeOf(err).Name()`).** Initial proposal during clarify; rejected because the README's "no runtime reflection" principle applies to all production code, not just typed field access. The closure-based `errors.As` approach gives the same dispatch behavior with stdlib doing the reflection.
- **Method on error type (`ErrorTypeName() string`).** Considered as a no-reflect, no-extra-registration alternative. Rejected because it forces every error type the developer wants to dispatch on to declare an extra method just for Writ — that's framework coupling on every error type, asymmetric with the lighter-weight `StatusCode() int` shape (which is also a method but is documented in `specs/errors.md` as the canonical convention; `ErrorTypeName` is not).
- **Code generation for error dispatch.** The README's "Code generation over reflection" principle suggests this. Rejected for now because codegen is its own deferred future feature; blocking the errors-block runtime on codegen would invert the dependency order. When codegen lands, the dispatch path can be replaced with generated code that hits a typed switch — the spec-004 contract (registered types and formatters dispatching on `errors.As`) is the runtime fallback that codegen will replace.
- **Status precedence: errors-block formatter overrides `StatusCode`.** Considered: the formatter writes whatever status it wants and `StatusCode` is just a hint. Rejected ambiguity-side — the spec already says the formatter is free to write a different status; the runtime passes `StatusCode()`'s value as the resolved hint and the formatter calls `w.WriteHeader(data.Status())` in normal use. This is clearer than re-litigating precedence at every formatter site.
- **Wrapping the dispatcher's `ServeHTTP` error path in a separate file.** Considered moving all error-path code into `error_dispatch.go`. Rejected partially: the orchestration (`handleResolverError`) lives there, but the *trigger* (the `if err != nil` after each resolver call) stays in `dispatch.go` to keep the request-lifecycle reading top-to-bottom in one file.

### Known limitations

- **Sentinels and wrapped sentinels don't match.** Per Q8, sentinel errors (`errors.New("...")`) cannot be registered via `writ.ErrorType[T]` because the closure target type is `*errors.errorString` — a name no developer would write in the DSL. Sentinels fall through to the Q3 no-match fallback. Resolvers that consume third-party sentinels translate at the boundary.
- **No partial-results visibility.** Per Q9, `ErrorData` does not expose the partial resolver-results table. Diagnostic context goes into the typed error itself.
- **No global default formatter API.** Per Q5, customization of the no-match path requires `errors /* -> default fn` in the DSL. There is no Go-side `DefaultErrorFormatter` registration.
- **`StatusCode()` checked only on the immediate concrete type.** The runtime does not walk `Unwrap` to find a `StatusCode()` deep in an error chain. Wrapping a typed error blocks the status from surfacing unless the wrapper also implements `StatusCode()`.
- **Format-stage errors still produce the spec-003 generic 500.** Per spec 004's *Out of Scope*, only resolver errors flow through the new pipeline; formatter errors continue to follow spec 003's rules.
- **Asymmetric registration surface (`(*Writ).ErrorFormatter` is a method; `writ.ErrorType[T]` is a free function).** A Go-language constraint, not a design preference. Documented in the package comment.
- **No validation that the registered `T` in `ErrorType[T]` has any practical relationship with errors actually returned by registered resolvers.** The runtime registers the matcher; whether real resolver errors ever match it is a runtime property the framework can't statically verify without code generation.

## Open Questions Resolved

The spec's *Resolved Questions* section enumerates 11 decisions; this plan honors all of them:

- **Error formatter signature** (Q1) — `ErrorFormatterFunc` and `ErrorData` per the spec; `ErrorData` ships with `Err`, `Status`, `Request` accessors.
- **`StatusCode()` interface name** (Q2) — anonymous interface assertion; no exported `writ.StatusCoder`.
- **Default fallback when no errors-block match** (Q3) — `resolveStatus` returns a `hasStatusCode` flag; `handleResolverError` chooses between `writeStatusText` (status-coded plain text) and `write500` (the spec-003 generic body).
- **Type-name dispatch mechanism** (Q4 + Q5 collapsed) — `writ.ErrorType[T error]` generic registration storing closures over `errors.As`; production stays reflect-free.
- **Type names with package qualifiers** (Q6) — superseded by Q4's explicit-registration model; package collisions are a per-project naming decision.
- **Default catch-all formatter** (Q7) — no global API; `errors /* -> default fn` in the DSL only.
- **Request access in formatter** (Q8) — `ErrorData.Request()` accessor.
- **Validation of unreferenced registrations** (Q9) — silently accepted, symmetric with spec-003 success registrations.
- **Sentinel matching** (Q10) — out of scope.
- **Partial-results visibility** (Q11) — out of scope.
- **Effective error map equality testing** — determinism test asserts index-by-index `(typeName, isDefault, span)` equality; matcher and formatter function pointers are not compared.
- **Generic 500 fallback for empty default** — same pre-/post-write rules as spec 003.
