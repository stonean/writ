# 003 — Runtime Skeleton + HTTP Dispatch Plan

## Overview

The runtime skeleton is a single Go package, `writ`, sitting alongside `ast`, `parser`, and `pipeline`. It composes them into an end-to-end request path: a registration API, a `Load(path)` that runs parser + elaborator + a runtime-specific validation pass, a routing table compiled from the elaborated `*pipeline.Resolved`, and an `http.Handler` that dispatches each request through a small per-handler lifecycle (extract params → run resolves → run formatter → flush response).

The package contains three categories of code:

- **Public API surface** — five methods on a `Writ` instance (`Resolver`, `Formatter`, `Load`, `Handler`, `Run`), three exported value types (`Params`, `Results`, `Error`), and two named function types (`ResolverFunc`, `FormatterFunc`).
- **Compilation pass** — translates `*pipeline.Resolved` plus the registration tables into an immutable, lookup-ready `routingTable` of `*compiledRoute` values. Runs once in `Load`. Failures aggregate into a single `*Error` with entries iterable by `ErrorKind`.
- **Request path** — a `ServeHTTP` that walks the routing table, runs resolves sequentially, calls the formatter, and writes a runtime-owned 500 on failure.

The runtime owns no goroutines, no I/O outside `Load` and the `Handler` it returns, and no global state. A `Writ` instance is the unit of isolation; tests build a fresh one per case.

## Technical Decisions

### Single `writ` package at module root

Package path is `github.com/stonean/writ`. Files live in the module root (alongside `ast/`, `parser/`, `pipeline/`).

Rationale:

- The README example reads `import "github.com/stonean/writ"` and `w := writ.New()`. Honoring that import path is non-negotiable — it's the public contract this feature implements.
- Splitting into sub-packages (`writ/runtime`, `writ/dispatch`) would force consumers to learn the split and would expose internals the runtime would prefer to hide. The whole runtime is one cohesive piece behind five public methods; sub-packaging buys nothing.
- Files within the package split by responsibility (lifecycle, registration, routing, validation, dispatch). All non-public types are unexported and live alongside the public surface they support.

### Public API

```go
package writ

// Writ is a runtime instance. Construct with New, register resolvers and
// formatters, call Load to compile a .writ file, then call Handler or Run
// to serve.
type Writ struct { /* unexported */ }

// New returns a fresh runtime instance with no registrations and no
// loaded program.
func New() *Writ

// Resolver registers fn under name. Returns an error if name is already
// registered. Panics if called after Load.
func (w *Writ) Resolver(name string, fn ResolverFunc) error

// Formatter registers fn under name. Returns an error if name is already
// registered. Panics if called after Load.
func (w *Writ) Formatter(name string, fn FormatterFunc) error

// Load reads and compiles the .writ program at path. Returns the
// aggregate *Error on any parse, elaboration, or validation failure;
// the runtime is left unloaded.
func (w *Writ) Load(path string) error

// Handler returns the runtime as a net/http handler. Panics if Load
// has not been called. Safe to invoke from third-party middleware.
func (w *Writ) Handler() http.Handler

// Run loads path, then binds on the port from PORT (default 8080) and
// serves until interrupted. Run does not accept an address argument;
// callers needing a custom listener use Load + http.ListenAndServe.
func (w *Writ) Run(path string) error
```

```go
// ResolverFunc is the Go signature for `resolve` step implementations.
type ResolverFunc func(ctx context.Context, params Params) (any, error)

// FormatterFunc is the Go signature for `format` step implementations.
type FormatterFunc func(ctx context.Context, w http.ResponseWriter, data Results) error
```

These signatures and names are the contract the rest of the framework will hang off — codegen, the testing DSL, source adapters, and worker processes will all compose against them. They are the primary API stability commitment of this feature.

### `Params` accessor type

```go
type Params struct {
    values map[string]string
}

func (p Params) String(name string) string
func (p Params) Has(name string) bool
```

- `String(name)` returns the route-parameter value bound at request time. Missing key returns `""`. Startup validation already ensures every `:name` referenced in a resolver argument is declared in the handler's route, so a missing key at runtime can only happen via test-harness misuse.
- `Has(name)` is included for symmetry with `Results.Has`. It supports tests that want to assert on the parameter table shape without invoking `String`.
- The unexported `values` map is keyed by parameter name (no leading `:`). The runtime populates it once per request from the parsed route pattern + URL.
- No iterator methods. The accessor surface is intentionally minimal so codegen can later generate typed wrappers (`params.UserID()`) without competing with a generic iterator.

### `Results` accessor type

```go
type Results struct {
    values map[string]any
}

func (r Results) Get(name string) any
func (r Results) Has(name string) bool
```

- `Get(name)` returns the raw `any` resolver result. Missing key returns `nil`.
- `Has(name)` reports presence — useful when a formatter handles an optional resolve.
- The dispatcher constructs `Results` per request with the values restricted to the names listed in the format line's `with` clause. This is the runtime-side enforcement of the spec's "formatter receives only listed names" contract.
- Symmetric in shape with `Params` so the runtime API stays coherent.

### Startup error type

```go
type ErrorKind int

const (
    KindParseFailure ErrorKind = iota
    KindElaborationFailure
    KindUnregisteredResolver
    KindUnregisteredFormatter
    KindUnsupportedStage
    KindUndeclaredRouteParameter
    KindRouteAmbiguity
    KindMissingEnvVar
)

type Entry struct {
    Kind    ErrorKind
    Message string
    Span    ast.Span
    Spans   []ast.Span // additional spans for ambiguity entries
}

type Error struct {
    Entries []Entry
}

func (e *Error) Error() string
func (e *Error) Unwrap() []error // each entry as its own error
```

- `Error` is the only error type returned from `Load`. A consumer pattern-matches on `*writ.Error` to access the typed entry list.
- `Entry.Kind` matches the categories pinned in the spec (Q10).
- `Entry.Spans` carries multiple spans for ambiguity errors (`KindRouteAmbiguity` lists both conflicting handlers); single-site entries leave it nil.
- `Unwrap() []error` lets `errors.As` find specific kinds via a wrapper helper, but the primary access is `entries := myErr.Entries`.
- Entry ordering: parser errors first (in source order), then elaboration errors (in source order), then validation errors (in source order from the elaborated `Resolved.Handlers` walk).

### Lifecycle state machine

The `Writ` instance is a tiny state machine with three phases:

```text
stateInit  →  stateLoading  →  stateLoaded
                  ↓ (failure)
              stateInit
```

- `stateInit`: registrations accepted, `Load` allowed, `Handler` panics.
- `stateLoading`: a `Load` call is in progress on this instance. Concurrent `Load` panics. Registrations panic.
- `stateLoaded`: registrations panic, `Handler` returns the compiled handler, `Load` returns the "already loaded" error, `Run` returns the same.

Implementation: a `sync/atomic.Uint32` holding the state, plus a read-mutex around `compiledTable`. The atomic CAS handles `stateInit → stateLoading` and detects the concurrent-load case in one operation; readers (`Handler`, `ServeHTTP`) read the state and the table without contention.

Rationale:

- `sync.Mutex` would also work but adds Lock/Unlock overhead on every `ServeHTTP` to read the table. The atomic-state + immutable-table-after-load pattern lets `ServeHTTP` proceed lock-free after `Load` succeeds.
- The state field documents intent and produces clearer error messages than reading from multiple bools.
- Matches the runtime-immutable-after-startup pattern the spec already requires for the routing table.

### Compilation pipeline (`Load` internals)

`Load` runs five strictly-ordered passes against `path`:

1. **Parse** — `parser.Parse(path)`. Returns `*ast.Program` plus `[]parser.Error`. Any errors translate to `Entry{Kind: KindParseFailure, Message, Span}` records and short-circuit subsequent passes (compilation cannot proceed against a partial AST consumers can't trust). Parser-error spans are mapped 1:1.
2. **Elaborate** — `pipeline.Elaborate(prog)`. Returns `*pipeline.Resolved` plus `[]pipeline.Error`. Errors translate to `Entry{Kind: KindElaborationFailure, ...}` records and short-circuit; subsequent passes need a clean `*Resolved` to reason about handler envelopes.
3. **Validate** — walk `Resolved.Handlers`. For each handler:
   - For each `Stage`, check `StageKind`:
     - `StageResolve`: the `ResolveStage.Call().Name` must be registered as a `ResolverFunc`. Each `:name` argument must be one of the handler's route parameter names.
     - `StageFormat`: the `FormatStage.Template()` value (the template name written in the DSL — e.g. `users.list`) is the formatter name and must be registered as a `FormatterFunc`. Each name in the format line's `with` clause is checked against the handler's resolved variable set (the names of preceding `ResolveStage` entries) — but this check belongs to a future feature; the skeleton iteration trusts the elaborated `with` list and uses `Results.Get` to surface missing keys at runtime if a formatter probes one. (Pinned out of scope: see *Trade-offs, Known Limitations*.)
     - Anything else: emit `Entry{Kind: KindUnsupportedStage, Message, Span}`.
   - The handler must end in exactly one `format` step (which spec 002 already guarantees for the in-scope subset, but we re-check defensively because a future pipeline change could weaken that).
4. **Check route ambiguity** — group handlers by `(method, canonicalRoutePath)` (canonical path replaces parameter segment names with a sentinel `:` so `/users/:id` and `/users/:user_id` collide). Any group with >1 entry produces a `KindRouteAmbiguity` entry whose `Span` is the first handler and `Spans` is the rest.
5. **Compile routing table** — for each handler, build a `*compiledRoute`:

```go
type compiledRoute struct {
    method     string             // verbatim, uppercase
    segments   []ast.RouteSegment // parsed pattern segments, no trailing wildcard
    paramNames []string           // names declared in the route (for fast lookup)
    resolves   []resolveStep      // pre-resolved resolver pointers + arg name lists
    format     formatStep         // pre-resolved formatter pointer + with-list
    span       ast.Span           // for error attribution
}

type resolveStep struct {
    name      string       // variable name from `resolve <name> = ...`
    fn        ResolverFunc // resolved at compile time
    paramArgs []string     // route-param names listed in the call (e.g. ["id"])
}

type formatStep struct {
    template string        // formatter registration key
    fn       FormatterFunc // resolved at compile time
    with     []string      // names listed in the format line's `with` clause
}
```

If any error is collected at any pass, return `*Error{Entries: ...}` and leave the runtime in `stateInit`. Successful compilation transitions to `stateLoaded` and stores the table.

### Routing table layout

```go
type routingTable struct {
    byMethod map[string][]*compiledRoute // routes per method, declaration order
    methods  []string                    // sorted, for fast Allow construction per path
}
```

Per-request matching:

1. Look up `byMethod[req.Method]`. If absent, fall through to the path-only scan to compute `Allow`.
2. Iterate the slice in order. For each route, compare `len(req.path segments) == len(route.segments)`. If not, skip. Otherwise walk segment-by-segment: literal segments require equality; parameter segments bind the value; mismatch on any segment skips the route. First match wins (route ambiguity is already a startup error so this can only happen once).
3. On a path-only scan (after a method miss), iterate every method's routes; collect the methods whose path matches. Return a sorted distinct list for the `Allow` header. If empty, the response is a 404.

Method-only fast path: a slice of `*compiledRoute` is small per-method. For the skeleton iteration, linear scan is fine — codegen will replace this with a generated dispatch function in a later feature.

### Request lifecycle (`ServeHTTP`)

```go
func (w *Writ) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
    state := w.state.Load()
    if state != stateLoaded {
        // Should never happen: Handler() panics if not loaded, and the
        // returned handler is only callable after that. Keep a defensive
        // check that writes a 500 rather than panicking inside a request.
        write500(rw, "runtime not loaded")
        return
    }

    route, params, allow := w.table.match(req.Method, req.URL.Path)
    switch {
    case route == nil && len(allow) == 0:
        http.NotFound(rw, req)
        return
    case route == nil:
        rw.Header().Set("Allow", strings.Join(allow, ", "))
        http.Error(rw, "405 method not allowed", http.StatusMethodNotAllowed)
        return
    }

    ctx := req.Context()
    results := make(map[string]any, len(route.resolves))
    for _, step := range route.resolves {
        val, err := step.fn(ctx, paramsForCall(params, step.paramArgs))
        if err != nil {
            write500(rw, "resolver error")
            return
        }
        results[step.name] = val
    }

    formatterResults := buildResults(results, route.format.with)
    if err := route.format.fn(ctx, rw, formatterResults); err != nil {
        // Only writes 500 if the formatter has not started writing.
        if !rw.(*writeRecorder).hasWritten {
            write500(rw, "formatter error")
        }
        return
    }
}
```

Notes:

- `writeRecorder` is a thin `http.ResponseWriter` wrapper that tracks whether `WriteHeader` or `Write` has been called. The runtime wraps `rw` once, before invoking the formatter.
- `paramsForCall(params, paramArgs)` returns a `Params` whose `values` is the subset of the request's parameter table referenced by this resolver's `:name` args. (Per the resolver contract: a resolver sees only the params its DSL call lists, not the full request param table.)
- `buildResults(results, withList)` returns a `Results` whose `values` is the subset listed in the format line's `with` clause.
- `write500` writes `Content-Type: text/plain; charset=utf-8`, status 500, body `"500 Internal Server Error\n"`. No internal details leak.

### `Run(path)` convenience

```go
func (w *Writ) Run(path string) error {
    if err := w.Load(path); err != nil {
        return err
    }
    port := os.Getenv("PORT")
    if port == "" {
        port = defaultPort
    }
    return http.ListenAndServe(":"+port, w.Handler())
}

const defaultPort = "8080"
```

`Run` does not configure timeouts on the constructed `http.Server`. Callers needing timeouts use `Load` + their own `&http.Server{...}`. The `defaultPort` constant lives in this package per `system.md` constants convention — `PORT` is documented as a reserved env var with a default.

### Constants and env vars

The skeleton iteration introduces no new env vars; it consumes only the two already declared in `system.md`:

- `PORT` — read by `Run`, default `8080` (named constant `defaultPort`).
- `WRIT_ENV` — read at `New` time and stored on the `Writ` instance, default `"production"` (named constant `defaultWritEnv`). Not consulted by any in-scope code path; reserved for future Configuration spec.

No `.env.example` change in this iteration. The Configuration spec (per `specs/inbox.md`) will own that file.

### Error message conventions

Each `Entry.Message` is one line, ending without a period, suitable for line-by-line presentation:

| Kind | Format |
| --- | --- |
| `KindParseFailure` | `parser: <verbatim parser message>` |
| `KindElaborationFailure` | `elaboration: <verbatim elaborator message>` |
| `KindUnregisteredResolver` | `resolver "<name>" is not registered` |
| `KindUnregisteredFormatter` | `formatter "<name>" is not registered` |
| `KindUnsupportedStage` | `stage <kind> is not supported in the runtime skeleton; see specs/003-runtime-skeleton` |
| `KindUndeclaredRouteParameter` | `parameter ":<name>" is not declared in route <route>` |
| `KindRouteAmbiguity` | `<METHOD> <path> is declared by <N> handlers` |
| `KindMissingEnvVar` | `environment variable <NAME> is required and not set` (unused in this iteration) |

Match the parser/elaborator's `file:line:col: message` formatting in `Error.Error()` aggregate output.

### Determinism

- The routing table is built deterministically from `Resolved.Handlers` (which spec 002 documents as deterministic). Iteration order in `byMethod` slices follows handler declaration order in the AST.
- `Allow` headers sort alphabetically per the spec resolution.
- No `map` ranges in the hot path: the validator uses ordered slices, and per-request param/results maps are looked up by exact key.
- No goroutines, no time, no I/O outside `Load`.

A determinism test compiles the same source twice in two `Writ` instances and asserts equal route tables (method, path, span sequence, registered-fn name).

### Testing strategy

- **Public API tests** (`writ_test.go`) — `New`, `Resolver`, `Formatter`, `Load`, `Handler` lifecycle; double-load, post-load registration panics; `httptest.NewServer` end-to-end with simple resolver+formatter.
- **Routing tests** (`route_test.go`) — table tests covering exact match, parameter binding, segment-count mismatch, method mismatch (405 + sorted Allow), trailing slash strict.
- **Validation tests** (`validate_test.go`) — one test per `ErrorKind`, asserting both kind and message shape.
- **Aggregate error tests** (`error_test.go`) — single error wrapping multiple entries, `Error()` string format, `Unwrap()` returns per-entry errors, `errors.As` against an `*Error`.
- **Concurrent-load test** — a `sync.WaitGroup` of two goroutines calling `Load` on the same instance; one wins, one panics. Use `defer recover` to capture the panic and assert the message.
- **No-I/O determinism test** — analogous to spec 002's source-grep but covering `os.` (excluding `os.Getenv` allowlist), `time.`, `net.` (excluding `net/http`), and `go func` in non-test files. The runtime *does* import `os` and `net/http`; the grep allowlists those tokens precisely.
- **Acceptance test pass** (`acceptance_test.go`) — every checkbox in `spec.md` mapped to a passing test, grouped by spec section.
- **Smoke test** — `testdata/smoke.writ` with two handlers, served via `httptest.NewServer`. Asserts 200/JSON, 405/Allow, 404, parameter binding, and a 500 path from a deliberately-failing resolver.

Coverage target: per `AGENTS.md`, the `writ` package targets ≥ 80% (it's not pure logic since it touches `net/http`, but most of the validation and routing surface is). Aim for ≥ 90% on `error.go`, `validate.go`, `route.go` since they are pure logic.

### `gosec` HTTP-server timeout note

`gosec` raises `G114` when an `http.ListenAndServe` call uses a server without timeouts. The runtime resolution is "no per-request timeout" (Q12), which `gosec` will flag. Suppress with a `//nolint:gosec` comment plus a one-line reason citing the spec resolution; per `AGENTS.md`, transient `// nolint` is reserved for cases where suppression is the documented project decision.

Alternative considered: pass `&http.Server{Handler: w.Handler(), ReadHeaderTimeout: 30 * time.Second}` as a "minimum sane default." Rejected — adds an env-driven knob the skeleton was scoped to defer, and the spec explicitly says the runtime defers all timeout policy.

## Affected Files

| File | Action | Purpose |
| --- | --- | --- |
| `writ/doc.go` | Create | Package doc declaring `writ` as the runtime entry point |
| `writ/writ.go` | Create | `Writ` struct, `New`, lifecycle state, `Handler`, `Run`, `defaultPort`, `defaultWritEnv` |
| `writ/register.go` | Create | `ResolverFunc`, `FormatterFunc`, `Resolver`, `Formatter` registration with state checks |
| `writ/params.go` | Create | `Params` type and accessors |
| `writ/results.go` | Create | `Results` type and accessors |
| `writ/error.go` | Create | `Error`, `ErrorKind`, `Entry`, formatting, `Unwrap` |
| `writ/validate.go` | Create | Startup validation pass that walks `*pipeline.Resolved` and produces `Entry` records |
| `writ/route.go` | Create | `compiledRoute`, `routingTable`, route compilation, request matching, `Allow` construction |
| `writ/dispatch.go` | Create | `ServeHTTP`, `writeRecorder`, `paramsForCall`, `buildResults`, `write500` |
| `writ/load.go` | Create | `Load` orchestration: parse → elaborate → validate → check ambiguity → compile |
| `writ/writ_test.go` | Create | Public-API tests |
| `writ/register_test.go` | Create | Registration error / panic tests |
| `writ/route_test.go` | Create | Path matching, parameter binding, 405/Allow, trailing slash |
| `writ/validate_test.go` | Create | Per-kind validation tests |
| `writ/error_test.go` | Create | Aggregate error formatting and unwrap |
| `writ/dispatch_test.go` | Create | Request lifecycle tests via `httptest` |
| `writ/load_test.go` | Create | Lifecycle: double-load, concurrent-load panic, parse/elaborate failure short-circuit |
| `writ/determinism_test.go` | Create | Source-grep no-I/O guard + structural-equality test |
| `writ/acceptance_test.go` | Create | Every checkbox in spec.md |
| `writ/testdata/smoke.writ` | Create | Smoke-test fixture: two handlers exercising path params, multiple resolves, format |
| `README.md` | Modify | Update feature 003 status to `planned` (then later to `done`) |

The companion `data-model.md` defines the full public type surface plus the internal `compiledRoute` / `routingTable` shapes.

## Trade-offs

### Considered and rejected

- **Sub-package split (`writ/runtime`, `writ/dispatch`)** — would let internals live behind narrower exports. Rejected: the README pins `writ.New()` as the import path and entry point. Sub-packaging would either break that or require a top-level re-export shim. The whole runtime is one cohesive piece behind five public methods; sub-packaging buys nothing.
- **Trie-based router** — `httprouter`-style radix tree with O(segments) matching. Rejected for now: the skeleton iteration's typical app has tens of routes; linear scan per method is fine. A future codegen feature replaces this with a generated dispatch function anyway.
- **`Run(path, addr)` overload** — would let callers pass an explicit address. Rejected per spec resolution Q4: drops to `Load + http.ListenAndServe` for non-`PORT` cases. Two lines of standard `net/http` code is not worth a second public surface.
- **Mutex-protected route table read** — `sync.RWMutex` around the table on every request. Rejected: an `atomic.Pointer[routingTable]` is sufficient since the table is immutable after `Load` returns. Lock-free hot path matches the runtime's "no per-request synchronization" stance.
- **Returning `[]Error` from `Load`** (mirroring parser/elaborator) — would echo spec 001/002's error-slice contract. Rejected: `Load`'s caller writes `if err := w.Load(...); err != nil { ... }`, which is the idiomatic Go pattern. A single `*Error` aggregate is more natural here. The aggregate exposes `Entries` for consumers who need the typed list, so no information is lost.
- **Built-in panic recovery in `ServeHTTP`** — would catch resolver/formatter panics and turn them into 500s. Rejected per spec resolution: panic recovery is `net/http` middleware territory, and stdlib `http.Server` already turns unrecovered handler panics into 500s by default. Adding our own recovery would just duplicate the default with project-specific coupling.
- **Validating `with` names against preceding `resolve` step names** — at startup, ensure every name in `format X with a, b` corresponds to a resolved variable. Rejected for this iteration: spec 002's elaboration already produces the canonical resolve-step list; cross-checking it adds value but expands the scope of in-scope checks. Deferred to a future "Cross-stage name validation" feature; for this iteration, an unmatched `with` name surfaces as `Results.Get` returning `nil` — formatters that probe an absent key see it.

### Known limitations

- **`with`-list completeness is not validated.** A `format users.list with bogus` line where `bogus` was never resolved compiles cleanly; the formatter sees `data.Get("bogus") == nil`. The spec covers this with the cross-stage-validation deferral above. Future feature spec ("Cross-stage name validation") owns the closing of this gap.
- **No request-body parsing.** The runtime does not consume `req.Body`. Resolvers that need it can read it directly through `ctx`-scoped access (which doesn't exist yet); for the skeleton iteration, body-using resolvers are out of scope per the spec.
- **No structured request logging.** Failures write `500` with a generic body but produce no log line. The Logging feature (deferred to the `log` stage's future spec) owns this.
- **Single linear scan per method.** Worst-case match is O(routes × segments). With ~50 routes per method this is trivially fast; with thousands, codegen-driven dispatch would be needed. Out of scope.
- **No graceful shutdown.** `Run` calls `http.ListenAndServe` directly; SIGTERM does not drain in-flight requests. Callers needing graceful shutdown construct `&http.Server{}` themselves and call `Server.Shutdown`. Adding a built-in graceful-shutdown surface is its own future spec.
- **No content negotiation.** Multiple `format` lines per handler are not supported (the spec requires exactly one). Adding this is the Content Negotiation spec.

## Open Questions Resolved

The spec's *Resolved Questions* section enumerates 13 decisions; this plan honors all of them:

- **Loader API surface** (Q1) — `Load` + `Handler` + `Run` all exposed; planned as three methods on `Writ`.
- **Resolver argument shape** (Q2) — `Params` accessor type with `String(name)` and `Has(name)`; concrete struct in `params.go`.
- **Formatter named-result type** (Q3) — `Results` accessor type with `Get(name)` and `Has(name)`; concrete struct in `results.go`.
- **Listener convenience method** (Q4) — `Run(path string) error` only; reads `PORT` via `os.Getenv` with `defaultPort` fallback.
- **`.env` and `WRIT_ENV` mode behavior** (Q5) — Deferred per the inbox; `WRIT_ENV` is read into the instance but does not gate any in-scope code path.
- **Default Content-Type on success** (Q6) — Runtime touches headers only on the runtime-owned 500 (`text/plain; charset=utf-8`); success path defers to `net/http` defaults.
- **`Allow` header construction** (Q7) — Alphabetical sort built once per path during ambiguity check; reused at request time.
- **Trailing slash policy** (Q8) — Strict: segment-count mismatch is a route miss.
- **Test-helper surface** (Q9) — Deferred; the public `Load` + `Handler` + `httptest.NewServer` pattern is the supported testing path.
- **Structured startup error type** (Q10) — `*Error` aggregate with `Entry.Kind`; eight `ErrorKind` constants.
- **Idempotent registration for overrides** (Q11) — Replacement is always an error; tests use a fresh `New()` per case.
- **Handler timeout** (Q12) — `Run` constructs no `http.Server` knobs; callers compose their own timeouts via `Load + http.ListenAndServe`.
- **Concurrent `Load()` calls** (Q13) — Detected by atomic CAS on the lifecycle state; second concurrent `Load` panics with a clear message.
