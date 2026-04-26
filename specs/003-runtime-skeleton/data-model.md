# 003 — Runtime Skeleton Data Model

The runtime introduces no database tables or persistent storage. Its data model is the public Go API surface plus the internal compiled-routing structures the dispatcher walks per request. Both are documented here so consumers of the runtime and future feature plans have a single reference.

All exported types live in package `github.com/stonean/writ`. The package is documented as exported but unstable pre-1.0, matching the AST and pipeline packages.

## Public Types

### `Writ` (instance)

```go
type Writ struct {
    // unexported fields:
    state     atomic.Uint32                  // stateInit | stateLoading | stateLoaded
    resolvers map[string]ResolverFunc        // populated during stateInit
    formatters map[string]FormatterFunc      // populated during stateInit
    table     atomic.Pointer[routingTable]   // installed at the stateInit→stateLoaded transition
    writEnv   string                         // WRIT_ENV value snapshotted at New
}
```

Lifecycle: `New` → `stateInit`. Transitions to `stateLoading` on `Load` entry; back to `stateInit` on `Load` failure or forward to `stateLoaded` on success. Once `stateLoaded`, `table` is read-only and lock-free.

### `ResolverFunc` and `FormatterFunc`

```go
type ResolverFunc func(ctx context.Context, params Params) (any, error)

type FormatterFunc func(ctx context.Context, w http.ResponseWriter, data Results) error
```

These are the only Go signatures the framework promises stability for in the skeleton iteration. Codegen, source adapters, and the testing DSL all compose against them.

### `Params`

```go
type Params struct {
    values map[string]string
}

func (p Params) String(name string) string
func (p Params) Has(name string) bool
```

- `values` is keyed by the parameter name as written in the route pattern (no leading `:`).
- The dispatcher constructs a fresh `Params` per resolver call, restricted to the names the DSL listed as arguments to that resolver. A resolver does not see route parameters it didn't ask for.
- Missing-key access (`String("nope")`) returns the zero value `""` rather than panicking. Test harnesses that bypass startup validation are the only realistic source of misses.

### `Results`

```go
type Results struct {
    values map[string]any
}

func (r Results) Get(name string) any
func (r Results) Has(name string) bool
```

- `values` is keyed by the resolve step's variable name. Iteration order is not exposed.
- The dispatcher constructs a fresh `Results` per request, restricted to the names listed in the format line's `with` clause.
- `Get` returns `nil` for absent keys. `Has` distinguishes "absent" from "present with `nil` value."

### `Error`, `Entry`, `ErrorKind`

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

func (k ErrorKind) String() string

type Entry struct {
    Kind    ErrorKind
    Message string
    Span    ast.Span    // primary span of the violation
    Spans   []ast.Span  // additional spans for ambiguity entries; nil for single-site
}

type Error struct {
    Entries []Entry
}

func (e *Error) Error() string         // multi-line "file:line:col: message" per entry
func (e *Error) Unwrap() []error       // each Entry as an individual error for errors.As/errors.Is
```

- `Error` is the only error type returned by `Load`. Consumers pattern-match with `var werr *writ.Error; errors.As(err, &werr)` to access `Entries`.
- `Entries` is in source order: parse errors first, then elaboration errors, then validation errors (each in source order from the elaborated `Resolved.Handlers` walk).
- `Unwrap` returns one `error` per `Entry`, each wrapping the kind and message. This lets `errors.Is(err, KindUnregisteredResolver)` work via a kind-as-error wrapper that the package implements internally.

## Internal Types

These are unexported. Consumers should not depend on them; they are documented here so the plan is concrete and reviewers can spot mismatches.

### `routingTable`

```go
type routingTable struct {
    byMethod map[string][]*compiledRoute // routes registered for each HTTP method, declaration order
    methods  []string                    // sorted (for Allow header construction)
}

func (t *routingTable) match(method, path string) (route *compiledRoute, params Params, allow []string)
```

- `byMethod` keys are verbatim (uppercase) HTTP method strings.
- `methods` is the sorted distinct list of declared methods, used to produce the `Allow` header on a 405 without re-walking every route.
- `match` returns the compiled route and bound params on a hit; `route == nil` plus a non-nil `allow` indicates a 405 (path matched some route but not on this method); `route == nil` with `allow == nil` indicates a 404.

### `compiledRoute`

```go
type compiledRoute struct {
    method     string             // verbatim, uppercase
    segments   []ast.RouteSegment // parsed pattern segments, no trailing wildcard
    paramNames []string           // names declared by parameter segments
    resolves   []resolveStep      // pre-resolved resolver pointers + arg lists
    format     formatStep         // pre-resolved formatter pointer + with-list
    span       ast.Span           // for error attribution and tooling
}
```

- `segments` mirrors the parser's `*ast.RoutePattern.Segments` slice for the handler's pattern. Trailing wildcards are not present in handler routes per spec 001's parser rules.
- `paramNames` is precomputed at compile time so `match` doesn't re-walk the segment slice to extract names.

### `resolveStep`

```go
type resolveStep struct {
    name      string       // variable name from `resolve <name> = <call>`
    fn        ResolverFunc // resolved at compile time from the runtime's registration map
    paramArgs []string     // route-parameter names listed in the call (e.g., "id" from db.users(:id))
}
```

`paramArgs` is the in-scope argument shape: spec 003 limits resolver arguments to bare route-parameter `:name` references. Future iterations expand this with field references and typed bodies; those will introduce additional fields on this struct.

### `formatStep`

```go
type formatStep struct {
    template string        // formatter registration key (the DSL template name, e.g. "users.list")
    fn       FormatterFunc // resolved at compile time
    with     []string      // names listed in the format line's `with` clause; preserves declaration order
}
```

The `template` field is the formatter's registered name. Spec 003 keeps formatters as registered Go functions only; future template-loading features will introduce a richer dispatch (auto-formatter from disk vs registered Go fn).

### `writeRecorder`

```go
type writeRecorder struct {
    http.ResponseWriter
    hasWritten bool // true once WriteHeader or Write has been called
}

func (r *writeRecorder) WriteHeader(status int)
func (r *writeRecorder) Write(p []byte) (int, error)
```

The dispatcher wraps the inbound `http.ResponseWriter` once, before invoking the formatter. The wrapper's `hasWritten` flag is the runtime's "can we still write a 500?" signal: a formatter that returned an error before writing any bytes gets the runtime's 500; a formatter that already wrote bytes does not (the response is already in flight).

## Lifecycle State Constants

```go
const (
    stateInit    uint32 = iota // accepting registrations and Load
    stateLoading               // a Load call is in progress
    stateLoaded                // Load succeeded; serving allowed
)
```

Held in `Writ.state` as `atomic.Uint32`. Transitions:

| From | To | Trigger |
| --- | --- | --- |
| `stateInit` | `stateLoading` | `Load` enters; CAS-protected to detect concurrent Load |
| `stateLoading` | `stateLoaded` | `Load` succeeds (table installed) |
| `stateLoading` | `stateInit` | `Load` fails (entries returned, no state change beyond rolling back) |
| `stateLoaded` | — | terminal |

Concurrent CAS failure (`stateInit → stateLoading` while already `stateLoading`) panics with a message naming the concurrent-load violation per the spec.

## Constants

```go
const (
    defaultPort    = "8080"
    defaultWritEnv = "production"
)
```

Both are package-local constants per `system.md`'s constants convention. They back the two reserved env vars `system.md` documents (`PORT`, `WRIT_ENV`); the skeleton iteration consumes only these.

## Notes

- **Spans** on `Entry` reference the originating AST node, satisfying the spec's source-attribution requirements automatically. Parser errors carry their own spans; elaboration errors carry the elaborator's spans; validation errors point at the offending DSL construct (a stage statement, a route segment, a handler block, etc.).
- **`Error.Entries` ordering** is total and deterministic. Two `Load` calls on equivalent input produce equal `Entries` lists: same kinds, same spans, same messages, same order.
- **No persistence.** All runtime state lives on the `Writ` instance in memory. Dropping the instance reclaims everything; the runtime owns no goroutines, no timers, and no open files outside the `Load` call's parser-driven file reads.
- **Future-feature compatibility.** Adding a new `ErrorKind` is additive; consumers switching on `Kind` get a default branch on unknown constants. Adding fields to `compiledRoute`, `resolveStep`, or `formatStep` is invisible to consumers since those types are unexported.
