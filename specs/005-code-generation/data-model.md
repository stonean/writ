# 005 — Code Generation Data Model

The feature introduces three categories of data:

1. **Wire types** in the existing `writ` package — consumer-facing.
2. **Wire-discovery output** in the new `codegen/wire` package — internal.
3. **Generator IR** in the new `codegen` package — internal.

No database tables. No persisted state. Every structure here lives in process memory during a single `writ generate` invocation.

## Wire Types (consumer-facing)

The runtime package gains four exported types whose only purpose is to be parseable by the generator and type-checked by the Go compiler. Declarations have no runtime effect.

```go
package writ

// WireResolvers maps DSL resolver names to Go function values.
// The generator parses wire declarations at codegen time only; the
// runtime never reads them. Consumers assign instances to the blank
// identifier so the map literal is constructed-then-discarded:
//
//   var _ = writ.WireResolvers{"db.users": GetUserByID, ...}
//
// The right-hand-side identifier of every entry is type-checked by
// the Go compiler as part of normal compilation; the generator
// merely records the (DSL name -> Go identifier) mapping.
type WireResolvers map[string]any

// WireFormatters maps DSL formatter names to Go function values.
type WireFormatters map[string]any

// WireErrorFormatters maps DSL error-formatter names to Go function
// values used by `errors` block entries.
type WireErrorFormatters map[string]any

// WireErrorTypes maps DSL error-type names to zero-value error
// instances. Each entry's right-hand side is a zero-value composite
// literal of the consumer's Go error type (e.g., `NotFound{}`); the
// generator recovers the type via go/packages type info.
type WireErrorTypes map[string]any
```

### Why `map[string]any`

The wire types could have been typed maps (e.g., `map[string]ResolverFunc`). They are deliberately untyped because the right-hand side of each entry is a typed Go expression in source — the Go compiler enforces that every named identifier exists and is well-typed at the call site, which is all the wire block needs to guarantee. A tighter type would force consumers to wrap their domain functions in adapter literals at the wire site, defeating the wire block's role as a thin DSL→Go index.

### Why one map type per role

Four maps (rather than one `WireBindings` keyed on a tagged enum) keep each wire block self-describing in source. A reader of `writ.WireResolvers{...}` knows immediately that the right-hand sides are resolver functions; collapsing the four into one would force every entry to carry a role tag and slow visual scanning.

## Wire-Discovery Output

The `codegen/wire` package walks consumer source via `go/packages` and produces a `Map` keyed by wire-type role:

```go
package wire

// Map carries the result of parsing every wire declaration in a
// consumer package. Keys within each sub-map are DSL identifiers
// (the string literal on the left side of a wire entry); values
// are Go identifiers (the expression on the right side, recovered
// from the AST).
//
// The Source field on each Entry points at the wire entry's
// position in the consumer's source so diagnostics can name a
// specific file and line.
type Map struct {
    Resolvers       map[string]Entry
    Formatters      map[string]Entry
    ErrorFormatters map[string]Entry
    ErrorTypes      map[string]ErrorTypeEntry
}

// Entry is one wire-block entry: a DSL name bound to a Go function
// identifier.
type Entry struct {
    GoIdentifier string  // qualified Go identifier ("pkg.Name" or "Name")
    Source       Position
}

// ErrorTypeEntry adds the recovered Go type for the value
// expression. The generator emits `errors.As(err, &t<i>)` arms
// using GoTypeName; GoIdentifier is the original expression text
// (e.g., "NotFound{}") for diagnostic messages.
type ErrorTypeEntry struct {
    Entry
    GoTypeName string  // qualified Go type name from pkg.TypesInfo
}

// Position carries enough information to format a `file:line:col`
// diagnostic. Mirrors ast.Span semantically but is local to the
// wire package to avoid coupling with the DSL AST.
type Position struct {
    File   string
    Line   int
    Column int
}
```

A duplicate DSL key within a single wire-type sub-map (e.g., two entries for `db.users` in `WireResolvers`) is reported as `KindWireDuplicateKey` and aborts generation before any IR is built.

## Generator IR

The `codegen` package builds a `Plan` from `*pipeline.Resolved` plus a `wire.Map`. The IR is the input to emission and the unit of testing for emit logic.

```go
package codegen

// Plan is the full input to emission: every handler in the package
// plus the package-level metadata needed for the file header.
type Plan struct {
    PackageName string
    SourceFiles []string  // lexically sorted .writ filenames
    Handlers    []HandlerPlan  // sorted by (Method, PatternString)
}

// HandlerPlan is one declared handler's emission unit. Identifier
// is the root Go identifier (e.g., "GetTodosByID"); the per-handler
// request struct, registration function, and any internal helpers
// share this root.
type HandlerPlan struct {
    Method            string  // "GET", "POST", etc.
    Pattern           *ast.RoutePattern
    PatternString     string  // canonical "/todos/:id" form
    Identifier        string  // "GetTodosByID"
    RequestStructName string  // "GetTodosByIDRequest"
    RegisterFuncName  string  // "RegisterGetTodosByID"
    ParamSegments     []ParamSegment
    Resolves          []ResolveCallPlan
    Format            FormatPlan
    ErrorMap          []ErrorArmPlan
}

// ParamSegment names one ":name" segment in the route pattern.
// Type is "string" until a future spec introduces typed parameters.
type ParamSegment struct {
    Name string
    Type string  // currently always "string"
}

// ResolveCallPlan describes one `resolve <name> = <call>` step.
// GoFuncIdent is the wire-resolved Go identifier the closure
// adapter calls; an unresolved name is a generator diagnostic.
type ResolveCallPlan struct {
    Name        string  // DSL name introduced by `resolve`
    DSLCall     string  // "db.users.byID" — diagnostic only
    GoFuncIdent string
    Args        []ResolveArg
}

// ResolveArg is one argument to a resolve call. Kind distinguishes
// route-parameter arguments (read from the request struct) from
// field-reference arguments (read from prior resolve results) and
// from named-literal arguments (passed verbatim).
type ResolveArg struct {
    Kind ResolveArgKind
    Name string
    Path []string  // for FieldRef: ["user", "id"] in :user.id
    Lit  ast.Literal  // for NamedArg
}

type ResolveArgKind int

const (
    ResolveArgRouteParam ResolveArgKind = iota
    ResolveArgFieldRef
    ResolveArgNamedLit
)

// FormatPlan describes the terminal `format` (or `redirect`) step.
// GoFuncIdent is the wire-resolved formatter; for redirect, it is
// empty and URLTemplate carries the redirect target.
type FormatPlan struct {
    Kind        FormatKind  // FormatKindFormat | FormatKindRedirect
    Template    string      // formatter name (for Format) or empty
    GoFuncIdent string      // wire-resolved (for Format) or empty
    Data        []ast.NamedRef  // `with` clause references
    URLTemplate string      // for Redirect
}

type FormatKind int

const (
    FormatKindFormat FormatKind = iota
    FormatKindRedirect
)

// ErrorArmPlan is one entry in the generated errors.As chain. Order
// in HandlerPlan.ErrorMap matches pipeline.Handler.ErrorMap (most-
// specific-first, default last).
type ErrorArmPlan struct {
    DSLTypeName      string  // "NotFound" or "default"
    DSLFormatterName string  // "not_found.json"
    IsDefault        bool
    GoTypeName       string  // qualified Go type, empty for default
    GoFormatterIdent string  // wire-resolved error formatter
}
```

### Construction

```go
func BuildPlan(
    resolved *pipeline.Resolved,
    wires wire.Map,
    sourceFiles []string,
    packageName string,
) (*Plan, []Diagnostic)
```

Walks every `*pipeline.Handler`, computes the root identifier via `MakeIdentifier`, looks up Go identifiers in `wires`, and builds the `HandlerPlan`. Collects diagnostics for:

- DSL names referenced from the `.writ` source that have no wire entry (`KindWireMissingEntry`).
- Identifier collisions across handlers (`KindIdentifierCollision`).
- Wire entries that reference a Go identifier the type checker reported as undefined (`KindWireStaleIdentifier`, surfaced from `wire.Discover`).

A non-empty diagnostic slice means the `*Plan` is not safe to emit; the caller (CLI) discards it and reports the diagnostics.

### Determinism invariants

- `Plan.Handlers` is always sorted by `(Method, PatternString)` ASCII order.
- `Plan.SourceFiles` is lexically sorted.
- Within each `HandlerPlan`, `Resolves` and `ErrorMap` preserve the elaboration-time order from `pipeline.Handler` — already deterministic per spec 002.
- `Plan` is constructed without ranging over Go maps in unsorted order; `wire.Map` lookups are by key, never by iteration.

These invariants are enforced by tests that build a `Plan` twice from the same inputs and compare every field, plus golden-file emit tests covering the full pipeline.

## Notes

- **No new error codes in `specs/errors.md`.** The diagnostic kinds introduced here are codegen-time, not runtime; they are formatted as `file:line:col: kind: message` per parser/pipeline convention and never surface as HTTP responses.
- **No new events in `specs/events.md`.** The generator does not emit events.
- **No conflicts with existing data models.** Specs 001/002/003/004's data models are read-only inputs; nothing here renames or modifies their types.
