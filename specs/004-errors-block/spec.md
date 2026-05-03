---
status: done
dependencies: [001-dsl-parser, 002-pipeline-elaboration, 003-runtime-skeleton]
tags: []
---

# 004 — Errors Block Runtime

The errors block runtime turns a returned Go error into a properly-formatted HTTP response by consuming the effective error map already produced by spec 002. The DSL syntax (`errors /pattern -> Type formatter`) is parsed by spec 001 and resolved into a per-handler `ErrorMap` by spec 002; this feature wires that map into the runtime's request path so a `NotFound` returned from a resolver renders through `notFoundJSON` instead of falling through to the runtime's generic 500.

This is a strict upgrade to spec 003's error path. Spec 003 wrote a runtime-owned `500 Internal Server Error` for any non-nil resolver or formatter error. This spec replaces that behavior for any handler whose effective error map contains an entry that matches the returned error type, while keeping the generic 500 as the fallback when no match exists.

## In Scope

The runtime serves an error response end-to-end when:

- A handler is loaded from a `.writ` file that contains one or more `errors /pattern ->` blocks. Spec 002 has already chosen the effective error map per handler and exposed it on `pipeline.Handler.ErrorMap`.
- A `resolve` step returns a non-nil error.
- The error's Go type name matches an entry in the handler's effective error map (or matches the `default` catch-all).
- The matched entry's formatter name resolves to a registered Go error-formatter function.
- The runtime invokes that formatter with the error value plus a status code derived from the error's `StatusCode()` method (when present); the formatter writes the response body and any additional headers it needs.

A handler whose effective error map does not match the returned type and does not declare a `default` falls back to the runtime-owned 500 from spec 003. This preserves backward compatibility — existing programs without an `errors` block continue to work unchanged.

## Out of Scope

Each item below has its own future feature spec or remains explicitly deferred.

- **Error handling on the format stage.** Formatter errors continue to follow spec 003's rules: pre-write errors produce the runtime-owned 500; post-write errors leave the partial response in flight. The errors block applies to resolver errors only in this iteration. (Rationale: a formatter that already started writing cannot be unwound; rendering an error response on top of a partial body would corrupt the wire.)
- **Runtime logging.** When an error short-circuits the request, the runtime continues to emit no log line. The `log` stage's future spec owns request-side observability.
- **Errors-block validation beyond reference completeness.** The runtime checks that every formatter name in the effective error map is registered. It does not verify that error type names correspond to real Go types — that requires code generation (deferred to a future feature).
- **Wrapped errors via `errors.Unwrap` / `errors.Is` / `errors.As`.** Type matching uses the immediate Go type name of the returned error. A wrapped error must be unwrapped by the caller before being returned. This iteration may revisit if a real consumer needs wrapping.
- **Multiple formatters per type via content negotiation.** The errors block maps each type to a single formatter name; multi-format error responses are deferred.
- **Custom `StatusCode` interface variants.** The runtime recognizes exactly one method shape (resolved in *Open Questions*); other status-conveying conventions (HTTP-aware error wrappers, gRPC-style codes, etc.) are out of scope.
- **Errors emitted by stages other than `resolve` and `format`.** Other pipeline stages remain out of scope per spec 003.
- **`writ show <route>` rendering of effective error maps.** CLI surface remains out of scope per spec 003.
- **Global default error formatter API (e.g., `w.DefaultErrorFormatter`).** Consumers who want custom no-match rendering write `errors /* -> default fn` in the DSL. There is no parallel Go-side global default registration; the DSL is the single source of truth for error rendering.
- **Sentinel error matching via `errors.Is`.** The runtime exposes one matching mechanism (`errors.As` via `writ.ErrorType[T]`). Resolvers that consume libraries returning sentinels (`pgx.ErrNoRows`, `io.EOF`, etc.) translate them to typed errors at the boundary before returning. A future feature spec can layer sentinel support on (likely shape: `writ.ErrorSentinel(name, sentinel)` using `errors.Is`); adding it is purely additive.
- **Partial resolver results in error formatters.** When a resolver in a multi-step handler errors, the prior resolves' results are not visible to the error formatter. `ErrorData` exposes only `Err()`, `Status()`, and `Request()`. Resolvers that need to surface diagnostic state (the failing step, the partial query) wrap that context into the typed error itself. A future feature can add partial-results visibility if a real consumer surfaces.

## DSL Recap

Spec 001 already parses the syntax; spec 002 already chooses the matching block per handler. The DSL form is:

```text
errors /admin/* ->
  NotFound        notFoundJSON
  Validation      validationJSON
  default         errorJSON
```

The left identifier names a Go error type; `default` is the catch-all. The right identifier names a registered error-formatter. Route patterns layer most-specific-first per spec 002's effective error map rules.

## Error Formatter Contract

An error formatter is a Go function distinct from the success-path formatters introduced in spec 003. The signature is symmetric with `FormatterFunc`, taking a typed accessor parameter that carries the error, the resolved status, and the originating request:

```go
type ErrorFormatterFunc func(ctx context.Context, w http.ResponseWriter, data ErrorData) error
```

`ErrorData` exposes:

- `Err() error` — the value the resolver returned. May be a typed struct or a wrapped error; `errors.As` and `errors.Is` work on it.
- `Status() int` — the status the runtime resolved per *Status Resolution* below.
- `Request() *http.Request` — the originating request, for formatters that need to read URL, method, headers, or set headers like `Retry-After`.

Future iterations may add accessors (parsed body, partial resolver results, request-id) without changing the signature.

The contract:

- **Outputs.** The formatter writes the response. Returning a non-nil error from an error formatter that has not yet written produces a runtime-owned 500 with the same generic body and `Content-Type: text/plain; charset=utf-8` that spec 003 emits today. Errors after writing leave the partial response in flight.
- **Headers.** The runtime never touches response headers on the error path. The formatter sets `Content-Type` and any others it needs. The runtime-owned 500 fallback continues to set `Content-Type: text/plain; charset=utf-8` because it owns those bytes.
- **Status.** The runtime does not pre-write the status. The formatter calls `w.WriteHeader(data.Status())` (or `http.Error`-equivalent) using the status the runtime resolved.
- **Default formatter.** The `default` keyword binds the catch-all to a single formatter name. There is no implicit "render anything" default formatter; if the effective error map's matched entry is `default`, that entry's formatter name must be registered.

## Status Resolution

When a resolver returns a non-nil error, the runtime resolves an HTTP status code as follows:

1. If the error implements the documented `StatusCode() int` method (per `specs/errors.md`), use the value it returns.
2. Otherwise, use 500.

The resolved status is passed to the error formatter (when an errors-block entry matches) as a hint. The formatter is free to write a different status (for instance, an error formatter that wraps the response in a JSON envelope might use 422 for validation regardless of the type's `StatusCode()` value), but in normal use the formatter calls `w.WriteHeader(status)` with the value the runtime resolved.

The four-state behavior matrix is:

| Errors-block match? | `StatusCode()` present? | Response |
| --- | --- | --- |
| Yes | Yes | Registered formatter writes the body; receives `Status()` from `StatusCode()`. |
| Yes | No | Registered formatter writes the body; receives `Status()` of 500. |
| No | Yes | Runtime writes `<status> <reason>\n` plain text (e.g., `404 Not Found\n`) with `Content-Type: text/plain; charset=utf-8`. |
| No | No | Runtime writes the spec-003 generic 500 with `Content-Type: text/plain; charset=utf-8` and body `500 Internal Server Error\n`. |

The runtime-owned text response in the "No / Yes" cell uses `http.StatusText(code)` for the reason phrase. This honors `StatusCode()` as a per-error opt-in for status, independent of whether the developer declared an `errors` block. Body customization (the errors block) and status customization (the error type's `StatusCode()` method) are separate concerns.

## Type Matching

The runtime matches a returned error to an `ErrorMap` entry by *registered Go type*. Each error type the developer wants the DSL to dispatch on must be registered:

```go
writ.ErrorType[NotFound](w, "NotFound")
writ.ErrorType[Validation](w, "Validation")
```

This top-level generic helper stores a closure-based matcher under the given name. The closure uses `errors.As(err, &target)` from the standard library, so:

- A returned value of the registered form matches: `ErrorType[NotFound]` matches a returned `NotFound{...}` value; `ErrorType[*NotFound]` matches a returned `*NotFound{...}` pointer.
- A wrapped error `fmt.Errorf("loading user: %w", NotFound{...})` matches the registration whose form aligns with the wrapped value because `errors.As` walks the `Unwrap` chain.
- An error of an unregistered type — or of the registered type but in the other form (value vs pointer) — does not match. `errors.As` is not pointer-vs-value uniform; resolvers that sometimes return the value and sometimes the pointer require either two registrations or a project convention to always return one form.

Writ's production code imports no `reflect` package; stdlib's `errors.As` performs the reflection internally.

Dispatch walks the handler's effective `ErrorMap` (already in most-specific-first order per spec 002):

- For each non-default entry: invoke the registered matcher for that `TypeName`. If it returns `true` for the error, the entry matches.
- If no concrete-type entry matches, walk again and use the first entry where `IsDefault` is true. The `default` entry matches any error.
- If no entry matches and there is no `default`, fall through to the *Status Resolution* matrix's "No errors-block match" rows: the runtime writes the resolved status plus a generic plain-text body, honoring `StatusCode()` if present.

Type-name dispatch is unambiguous: the developer registers exactly one type per name; collisions across packages (e.g., `pkg1.NotFound` vs `pkg2.NotFound`) require distinct names in the DSL because each registration call binds one Go type to one DSL identifier.

## Registration

The runtime exposes two new registration entry points:

```text
(*Writ).ErrorFormatter(name, fn) → error
writ.ErrorType[T error](w *Writ, name string) → error
```

`ErrorFormatter` is a method on `Writ` (consistent with `Resolver` and `Formatter` from spec 003). `ErrorType` is a top-level generic function because Go does not permit type parameters on methods; it accepts the runtime instance as its first argument.

Both follow the same rules already established in spec 003:

- Return an error if `name` is already registered in their respective table.
- Panic if called outside `stateInit`.
- Idempotent re-registration is not allowed; tests build a fresh `New()` per case.

Error formatters and success formatters are registered in distinct namespaces. A name `errorJSON` registered as an error formatter is independent of any name `errorJSON` registered as a success formatter; the runtime never looks up an error formatter in the success-formatter table or vice versa. The error-type registry is a third independent namespace keyed by the DSL identifier (the left side of an `errors` block entry). Three separate namespaces keep the per-stage contract clean and avoid ambiguity at registration sites.

## Validation

In addition to the validation passes from spec 003, the runtime adds:

- **Reference completeness (error formatters).** Every formatter name referenced in any handler's effective error map is registered as an error formatter. Unregistered names produce a structured startup error naming the formatter and the source span of the offending `errors` block entry.
- **Reference completeness (error types).** Every non-default `TypeName` referenced in any handler's effective error map has a registered Go type via `writ.ErrorType[T]`. Unregistered names produce a structured startup error naming the type and the source span of the offending `errors` block entry. The `default` keyword is exempt from this check (no Go type to register).

The structured error type from spec 003 gains two new kinds:

- `KindUnregisteredErrorFormatter` — error formatter name in the DSL has no registration.
- `KindUnregisteredErrorType` — non-default error type name in the DSL has no registered Go type.

All other spec-003 validation rules continue to apply unchanged. Existing programs without `errors` blocks elaborate to a handler with an empty `ErrorMap`, and the new validation passes walk zero entries.

## Request Lifecycle Update

Spec 003's request lifecycle becomes:

1. Match the request against the routing table (unchanged).
2. Run resolves sequentially (unchanged).
3. **If a resolve returns a non-nil error:**
   1. Resolve the status code per *Status Resolution*.
   2. Look up the error in the handler's effective `ErrorMap` per *Type Matching*.
   3. If a match exists, invoke the registered error formatter with the error value and resolved status.
   4. If no match exists, fall through to the spec-003 runtime-owned 500.
4. Run the format step (unchanged).

Steps 1, 2, and 4 are byte-identical to spec 003. Step 3 is the entire delta.

## Acceptance Criteria

### Registration

- [x] `w.ErrorFormatter(name, fn)` registers an error formatter under `name` and returns nil on success.
- [x] Registering the same `name` a second time as an error formatter returns an error.
- [x] An error formatter and a success formatter may share the same `name` without collision.
- [x] Calling `ErrorFormatter` after `Load` panics with a clear message.
- [x] `writ.ErrorType[T error](w, name)` registers an error type under `name` and returns nil on success.
- [x] Registering the same `name` a second time as an error type returns an error.
- [x] Calling `writ.ErrorType` after `Load` panics with a clear message.
- [x] The error-type registry, error-formatter registry, and success-formatter registry are independent namespaces; the same `name` may appear in all three without collision.

### Validation

- [x] Loading a `.writ` file whose `errors` block names a formatter that is not registered as an error formatter fails with a `KindUnregisteredErrorFormatter` entry naming the formatter and the source span.
- [x] Loading a `.writ` file with no `errors` block passes the new validation passes with zero entries (regression check).
- [x] Loading a `.writ` file whose `errors` block formatter is registered only as a success formatter (not an error formatter) fails with `KindUnregisteredErrorFormatter`.
- [x] Loading a `.writ` file whose `errors` block names a non-default type that has no registered Go type fails with a `KindUnregisteredErrorType` entry naming the type and the source span.
- [x] The `default` keyword in an `errors` block is exempt from the error-type registration check.

### Status resolution

- [x] An error implementing `StatusCode() int` is rendered with the returned status when an errors-block entry matches.
- [x] An error not implementing `StatusCode() int` is rendered with status 500 when an errors-block entry matches.
- [x] Status resolution does not run when no errors-block entry matches; the runtime falls through to the spec-003 generic 500 with `Content-Type: text/plain; charset=utf-8`.

### Type matching

- [x] A resolver returning a `NotFound{...}` value matches an `errors /* -> NotFound notFoundJSON` entry when `writ.ErrorType[NotFound](w, "NotFound")` is registered; the registered `notFoundJSON` is invoked.
- [x] A resolver returning a `*NotFound{...}` pointer matches an `errors /* -> NotFound notFoundJSON` entry when `writ.ErrorType[*NotFound](w, "NotFound")` is registered (the pointer form must be registered separately; `errors.As` is not pointer-vs-value uniform).
- [x] A resolver returning `fmt.Errorf("loading user: %w", NotFound{...})` matches an `ErrorType[NotFound]` registration (`errors.As` walks the `Unwrap` chain).
- [x] When a handler's effective error map contains both `NotFound` and `default`, returning a `NotFound` matches the specific entry; returning a different type matches `default`.
- [x] When the effective error map contains only `default`, every returned error type matches the default entry — including errors with no registered type.
- [x] When the effective error map is empty and no entry matches, the request falls through to the *Status Resolution* matrix's "No errors-block match" rows.

### Error formatter dispatch

- [x] The error formatter receives the request context, the response writer, and an `ErrorData` accessor whose `Err()`, `Status()`, and `Request()` methods return the originating error, the resolved status, and the originating `*http.Request` respectively.
- [x] An error formatter that writes a 404 body produces a 404 response with that body.
- [x] An error formatter that returns an error before writing produces the runtime-owned 500 with `Content-Type: text/plain; charset=utf-8`.
- [x] An error formatter that returns an error after writing leaves the partial response in flight.
- [x] The `Request()` accessor returns the same `*http.Request` the dispatcher received from `net/http`.

### Backward compatibility

- [x] A program loaded under spec 003 that does not declare any `errors` block continues to behave identically: resolver errors produce the runtime-owned 500 with the same generic body and headers.
- [x] A program that registers an `ErrorFormatter` but does not declare any `errors` block is loaded successfully; the registered formatter is simply unused (no warning, no error — symmetric with success formatters that are registered but unreferenced).

### Source provenance

- [x] When a `KindUnregisteredErrorFormatter` entry is reported, its `Span` references the originating `errors` block entry's source location.

### Determinism

- [x] Loading a program with an `errors` block twice produces structurally equal compiled error-handler tables: same per-handler entry count, same `(TypeName, formatter-function-name, IsDefault)` tuple at each index, and same `Span` at each index. The order of entries (most-specific-first per spec 002) is preserved across loads.

## Open Questions

*All open questions resolved.*

## Resolved Questions

- **Error formatter signature exact spelling** — Typed accessor: `func(ctx context.Context, w http.ResponseWriter, data ErrorData) error`, where `ErrorData` carries `Err() error` and `Status() int` accessors plus room for future fields. Rationale: continues the `Params`/`Results` accessor pattern from spec 003 — three parallel accessor types (`Params`, `Results`, `ErrorData`) keep the framework's formatter surface coherent. Future additions (request access, partial-results visibility, parsed-body access for validation errors) become additive method additions on `ErrorData` rather than breaking signature changes. Matches the user's preference for uniform extension over per-feature special cases. Impacts Q7 (request access) and Q10 (partial results) — both become "add a method to `ErrorData`" rather than signature changes.
- **`StatusCode()` interface name** — Unnamed interface check. The runtime detects the method shape inline via `if sc, ok := err.(interface{ StatusCode() int }); ok { ... }`. No exported `writ.StatusCoder` type. Rationale: implementing the shape requires zero `writ` imports — error types stay self-contained and portable across frameworks. The method shape *is* the contract; a named interface adds a synonym, not a new constraint. Idiomatic Go precedent (`error` itself, anonymous interface assertions throughout the standard library) supports duck-typing for cross-cutting method shapes that don't depend on framework types. Same consistency principle as Q1: avoid per-feature special cases when a uniform convention applies.
- **Default fallback when no errors-block entry matches** — Honor `StatusCode()` even without an errors-block match. The runtime writes `<status> <reason>\n` plain text (using `http.StatusText`) with `Content-Type: text/plain; charset=utf-8`. When `StatusCode()` is absent, the existing spec-003 generic 500 path is unchanged. Body customization (the errors block) and status customization (the error type's `StatusCode()` method) are independent concerns; ignoring `StatusCode()` because the developer didn't *also* write an `errors` block would punish the most common minimal setup (typed errors, no custom body shape). The resulting four-state behavior matrix is documented in the *Status Resolution* section.
- **Type-name dispatch mechanism** — Use generic registration with `errors.As` matching. The runtime exposes a top-level helper `writ.ErrorType[T error](w *Writ, name string) error` that registers a closure-based matcher; the matcher uses `errors.As` from the standard library. Writ's own production code imports no `reflect` package, preserving the README's "no runtime reflection" principle. Rationale: the README's principle prohibits `reflect` in production code; a quick audit confirms only test files currently use it. Stdlib's `errors.As` performs its own reflection internally, but that's stdlib behavior — Writ stays reflect-free. Generics provide the type information the registration needs. Wrapped errors via `fmt.Errorf("...: %w", err)` are matched because `errors.As` walks the `Unwrap` chain. **Known limitation:** `errors.As` is not pointer-vs-value uniform; `ErrorType[NotFound]` matches `NotFound{}` returns but not `*NotFound{}` returns, and vice versa. Resolvers that mix forms require two registrations or a project convention. The trade-off is a new registration call in user code that the README's current example doesn't show, plus a documentation update to teach the `writ.ErrorType[T]` pattern and the value-vs-pointer rule.
- **Type names with package qualifiers** — Resolved by the new dispatch mechanism. With explicit `writ.ErrorType[T](w, name)` registration, the developer chooses the DSL identifier and binds it to one Go type. Two `NotFound` types from different packages register under distinct DSL names (e.g., `"PkgOneNotFound"` and `"PkgTwoNotFound"`); the DSL `errors` block uses whichever names the developer chose. Duplicate name registrations are already a startup error per Q1, so collision is impossible at the framework level — naming convention is a per-project decision the developer makes. The DSL grammar accepts any identifier; no grammar change is needed. The single-package "Just Works" path (`errors /* -> NotFound notFoundJSON` paired with one `writ.ErrorType[NotFound]`) is unchanged.
- **Default catch-all formatter without an explicit `default` entry** — No global default API. Consumers who want custom no-match rendering write `errors /* -> default fn` in the DSL; the runtime exposes no `DefaultErrorFormatter` registration. Rationale: a parallel Go-side global default would create two ways to do the same thing — a developer reading the `.writ` file would see "no errors block, generic 500" while actual behavior depended on whether the global default was registered in `main.go`, exactly the kind of invisible coupling the framework's "convention over configuration" stance opposes. Success formatters have no parallel global default; errors should follow the same shape. The runtime-owned 500 from Q3 remains the universal fallback when no errors block declares anything, so "I haven't gotten around to error handling" works without boilerplate. Recorded explicitly in *Out of Scope*.
- **Error formatter receives request method/path** — Add `Request() *http.Request` to `ErrorData`. The accessor type already accepted in Q1 is the natural home — adding the request is a method addition, not a signature change. Rationale: the use cases are real and varied (Retry-After headers based on path, request-id surfaced in error bodies, structured log lines with method+path, content-type sniffing for negotiated error responses); forcing every error formatter that wants any of these to fish them out of `context.Context` is more API surface than just exposing the request. Spec 003's resolver contract keeps `*http.Request` out because resolvers don't need it, but error-formatter surface is genuinely wider (writing the response and reasoning about why the request failed) — giving it the request is consistent with its job, not a leak. Honors Q1's "additions become accessor methods, not signature changes" trade-off. The dispatcher passes `req` into `ErrorData` at construction time; `ServeHTTP(rw, req)` already has it in scope.
- **Validation of unreferenced error formatters** — Silently accept. No warning, no error, symmetric with how spec 003 treats unreferenced success formatters and resolvers. The same rule applies to unreferenced error-type registrations (`writ.ErrorType[Validation]` registered without any `errors` block referencing `Validation`). Rationale: spec 003 already established silent-accept for unreferenced registrations; diverging here would be the inconsistency. The parser/elaborator/runtime have collectively decided "errors only, no warnings"; introducing the first warning category here opens debates the project has so far avoided. A real consumer pattern is registering a library of error formatters once and only some `.writ` files using all of them; forcing every program to use every registered formatter would discourage shared registration helpers. A future tooling feature (`writ doctor` or similar) is the right home for registry-vs-DSL completeness diagnostics.
- **Integration with `errors.Is` for sentinel errors** — Out of scope. The runtime exposes one matching mechanism (`errors.As` via `writ.ErrorType[T]`); resolvers that consume libraries returning sentinels (`pgx.ErrNoRows`, `io.EOF`, etc.) translate them to typed errors at the boundary before returning. Rationale: two parallel matching mechanisms in one iteration doubles the conceptual surface; single mechanism plus a future feature when justified is the staged approach the constitution prefers. Sentinels-as-public-API is a debatable design choice — most Go style guides recommend typed errors for any error that crosses package boundaries because typed errors carry context. Boundary translation aligns with the better-established style. A future feature spec can lift this out-of-scope when a real consumer surfaces (likely shape: `writ.ErrorSentinel(name, sentinel)` using `errors.Is`); adding it is purely additive. Recorded explicitly in *Out of Scope*.
- **Errors during resolve in a multi-resolve handler** — Partial resolver results are not exposed to error formatters in this iteration. `ErrorData` carries only the error, the resolved status, and the request. Resolvers that need to surface diagnostic state (the failing step, the partial query) wrap that context into the typed error itself; the framework gives the formatter the error verbatim, so consumers shape the error type to carry whatever they need. Rationale: the success-path `Results` projection in spec 003 was deliberate (formatters see only names listed in `with`); exposing partial results to error formatters breaks that projection — error formatters would have access to data success formatters cannot see. Adding partial-results visibility also creates a contract dependency on declaration order that a future parallel-resolve feature would have to migrate. A future feature can lift this when a real consumer surfaces. Recorded explicitly in *Out of Scope*.
- **Effective error map equality testing** — Assert both the type-to-formatter mapping and the entry order. The determinism test walks each handler's compiled error table and asserts: same number of entries, same `(TypeName, formatter-function-name, IsDefault)` tuple at each index, same `Span` at each index. Rationale: spec 002 documents that the effective error map is in most-specific-first order, and the runtime consumes that order verbatim — first concrete match wins at request time. If a future change accidentally re-sorted entries, behavior would silently shift (a typo'd specific entry might silently fall through to `default`). Order is part of the contract; test it. Existing determinism criterion tightened to spell out the index-by-index check.
- **Generic 500 fallback behavior for empty `default`** — Fall through to the runtime-owned 500 with the same pre-/post-write semantics spec 003 already uses. No panic. Concretely: error formatter that returns an error before writing produces the runtime-owned 500; error formatter that errors after writing leaves the partial response in flight. Rationale: spec 003 established the symmetry; diverging here would be the inconsistency. Panicking would crash the server process and take down in-flight requests on other goroutines, which is louder than warranted for a runtime condition (formatter errors are typically bad data, not bad code). A future logging feature will surface formatter errors to operators without needing API changes here. Confirms the originally-proposed contract.
