# 002 — Pipeline Elaboration Data Model

The `pipeline` package produces a `Resolved` value: one entry per handler, each carrying an effective stage list, an effective error map, and explicit-opt-out markers. This document defines that structure.

All types live in package `github.com/stonean/writ/pipeline`. All exported types are documented as unstable pre-1.0 in the package comment.

## Resolved (Root)

```go
// Resolved is the per-handler effective-pipeline structure produced by Elaborate.
// Always non-nil after Elaborate; on partial failure, contains entries for every
// handler that elaborated cleanly.
type Resolved struct {
    Handlers []*Handler
}
```

`Handlers` is in source declaration order — the order `*ast.Program.Handlers` had at parse time. Iteration is deterministic.

## Handler

```go
type Handler struct {
    Method     string             // verbatim method token, e.g., "GET", "MKCOL"
    MethodSpan ast.Span           // method-token span from the source HandlerBlock
    Pattern    *ast.RoutePattern  // route pattern as parsed
    Stages     []Stage            // canonical-order effective pipeline (live stages only)
    OptOuts    []OptOut           // explicit `<stage> none` opt-outs at the winning level
    ErrorMap   []ErrorMapEntry    // effective error map; deterministic iteration order
    Span       ast.Span           // span of the source HandlerBlock
    Source     *ast.HandlerBlock  // back-pointer to the originating AST node
}
```

`Stages` is canonical-pipeline order with observational stages floated to their source positions (see *Stage Ordering* below). Single-instance stages appear at most once. Multi-instance stages may appear multiple times in cross-level order.

`OptOuts` records every stage kind that was explicitly cleared via `<kind> none` at the level whose declaration won. Each entry's `Span` points at the originating `NoneStmt`. The runtime distinguishes "stage not present" (kind absent from both `Stages` and `OptOuts`) from "stage explicitly removed" (kind present in `OptOuts`).

`ErrorMap` is a slice rather than a map so iteration is deterministic. Lookup by `TypeName` is linear; per-handler entry counts are small.

`Source` lets consumers walk back to the originating AST node for diagnostics that need block-level context.

## OptOut

```go
type OptOut struct {
    Kind StageKind
    Span ast.Span // span of the NoneStmt at the winning level
}
```

For multi-instance stages, `OptOut` records the most recent `<kind> none` across the precedence walk (the one that cleared the inherited list). For single-instance stages, `OptOut` records the `none` only when no later level redeclared the stage with a value.

## ErrorMapEntry

```go
type ErrorMapEntry struct {
    TypeName      string             // verbatim error-type identifier; "default" for the catch-all
    TypeSpan      ast.Span
    Formatter     string             // verbatim formatter identifier
    FormatterSpan ast.Span
    IsDefault     bool               // true when TypeName == "default"
    SourceBlock   *ast.ErrorsBlock   // the originating errors block; never nil
}
```

`SourceBlock` lets `writ show` and diagnostics name the block that contributed the entry. Per-entry source spans point at the originating `*ast.ErrorsEntry`'s `TypeSpan` and `FormatterSpan`; the entry that wins is the most-specific block's entry for that `TypeName`.

## StageKind

```go
type StageKind int

const (
    StageLog StageKind = iota
    StageMeasure
    StageSession
    StageCSRF
    StageLimit
    StageApprove
    StageResolve
    StageCommit
    StageEmit
    StageLayout
    StageFormat
    StageRedirect
)

// CanonicalPosition returns the canonical pipeline position of a semantic
// stage kind (1-based: session=1, csrf=2, ...). Returns 0 for the
// observational kinds (log, measure), which are exempt from canonical
// ordering.
func (k StageKind) CanonicalPosition() int

// IsObservational reports whether the kind is log or measure.
func (k StageKind) IsObservational() bool

// IsSingleInstance reports whether at most one declaration per level is
// allowed: session, csrf, limit, approve, layout. (format/redirect are
// terminators, handled separately.)
func (k StageKind) IsSingleInstance() bool

// IsMultiInstance reports whether multiple declarations per level are
// allowed: resolve, commit, emit. Observational kinds are also multi-
// instance but classified separately by IsObservational.
func (k StageKind) IsMultiInstance() bool

// IsTerminal reports whether the kind ends a pipeline: format, redirect.
func (k StageKind) IsTerminal() bool

func (k StageKind) String() string // "log", "session", "approve", ...
```

Predicate methods exist so the override engine and downstream tooling can categorize without hard-coding constant lists.

## SourceLevel

```go
type SourceLevel int

const (
    SourceSystem SourceLevel = iota
    SourceGroup
    SourceHandler
)

func (l SourceLevel) String() string // "system", "group", "handler"
```

Each `Stage` value carries a `SourceLevel` so consumers (`writ show <route>`, diagnostics) can render "inherited from system" / "from group `/admin/*`" / "declared on this handler" without re-walking the AST.

## Stage Interface

```go
// Stage is one entry in a Handler's effective pipeline. Concrete types
// expose accessors specific to their kind.
type Stage interface {
    Kind() StageKind
    Span() ast.Span                  // span of the originating AST statement
    SourceLevel() SourceLevel        // which precedence level contributed this stage
    SourceGroup() *ast.GroupBlock    // non-nil only when SourceLevel == SourceGroup
}
```

`Span()` is the span of the originating AST node (`LogStmt`, `ApproveStmt`, etc.). When a handler inherits `approve` from the system block, the span points at the system block's `ApproveStmt`.

`SourceGroup()` is nil for stages from the system block or the handler block, and the originating `*ast.GroupBlock` for stages that came from a group.

## Concrete Stage Types

Each concrete stage holds a pointer to its originating AST statement. Accessors thin-wrap AST fields rather than copying values into the stage.

```go
type LogStage struct {
    src   *ast.LogStmt
    level SourceLevel
    group *ast.GroupBlock
}
func (s *LogStage) Kind() StageKind             { return StageLog }
func (s *LogStage) Span() ast.Span              { return s.src.Span() }
func (s *LogStage) SourceLevel() SourceLevel    { return s.level }
func (s *LogStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *LogStage) Args() []ast.Expr             { return s.src.Args }

type MeasureStage struct { /* same shape as LogStage */ }
func (s *MeasureStage) Args() []ast.Expr         { return s.src.Args }

type SessionStage struct { /* shape as above */ }
func (s *SessionStage) Storage() string          { return s.src.Storage }
func (s *SessionStage) StorageSpan() ast.Span    { return s.src.StorageSpan }

type CSRFStage struct { /* shape as above */ }
func (s *CSRFStage) Mode() string                { return s.src.Mode }
func (s *CSRFStage) ModeSpan() ast.Span          { return s.src.ModeSpan }

type LimitStage struct { /* shape as above */ }
func (s *LimitStage) Call() *ast.Call            { return s.src.Call }

type ApproveStage struct { /* shape as above */ }
func (s *ApproveStage) Expr() ast.ApproveExpr    { return s.src.Expr }

type ResolveStage struct { /* shape as above */ }
func (s *ResolveStage) Name() string             { return s.src.Name }
func (s *ResolveStage) NameSpan() ast.Span       { return s.src.NameSpan }
func (s *ResolveStage) Call() *ast.Call          { return s.src.Call }

type CommitStage struct { /* shape as above */ }
func (s *CommitStage) Name() string              { return s.src.Name }
func (s *CommitStage) NameSpan() ast.Span        { return s.src.NameSpan }
func (s *CommitStage) Call() *ast.Call           { return s.src.Call }
func (s *CommitStage) IsFireAndForget() bool     { return s.src.Name == "" }

type EmitStage struct { /* shape as above */ }
func (s *EmitStage) Event() string               { return s.src.Event }
func (s *EmitStage) EventSpan() ast.Span         { return s.src.EventSpan }
func (s *EmitStage) Data() string                { return s.src.Data }
func (s *EmitStage) DataSpan() ast.Span          { return s.src.DataSpan }
func (s *EmitStage) HasData() bool               { return s.src.Data != "" }

type LayoutStage struct { /* shape as above */ }
func (s *LayoutStage) Name() string              { return s.src.Name }
func (s *LayoutStage) NameSpan() ast.Span        { return s.src.NameSpan }

type FormatStage struct { /* shape as above */ }
func (s *FormatStage) Template() string          { return s.src.Template }
func (s *FormatStage) TemplateSpan() ast.Span    { return s.src.TemplateSpan }
func (s *FormatStage) Data() []ast.NamedRef      { return s.src.Data }
func (s *FormatStage) Layout() string            { return s.src.Layout }
func (s *FormatStage) LayoutSpan() ast.Span      { return s.src.LayoutSpan }

type RedirectStage struct { /* shape as above */ }
func (s *RedirectStage) URL() string             { return s.src.URL }
func (s *RedirectStage) URLSpan() ast.Span       { return s.src.URLSpan }
```

The constructor signature is uniform across stage types — `newXxxStage(src *ast.XxxStmt, level SourceLevel, group *ast.GroupBlock)` — to keep the `level.go` and `override.go` builders concise.

## Stage Ordering in Handler.Stages

`Handler.Stages` is the effective pipeline in this order:

1. For each canonical position 1–9 (`session`, `csrf`, `limit`, `approve`, `resolve`, `commit`, `emit`, `layout`, `format`/`redirect`):
   - Single-instance positions emit zero or one stage (the surviving cross-level winner).
   - Multi-instance positions (`resolve`, `commit`, `emit`) emit all surviving steps in cross-level order: system steps, then steps from each kept group (least-specific first), then handler steps. Source declaration order is preserved within each level.
   - The terminal position emits zero, one, or many stages — `format` is multi-instance for content negotiation; `redirect` is single-instance and mutually exclusive with `format` (mixing is enforced by startup validation, not here).
2. Observational stages (`log`, `measure`) interleave at the canonical position they "belong to" within their level — the canonical position of the nearest preceding semantic statement at the same level, or the "before everything" slot if no preceding semantic statement exists. Cross-level ordering: observationals from system appear before observationals from groups, which appear before observationals from the handler, when they share a canonical anchor.

The observational rule is what makes the spec's example produce `log request_received`, `approve`, `resolve user`, `log user_loaded`, `resolve posts`, `log posts_loaded`, `format` rather than collapsing the logs to one position.

## Notes

- **All `Span` values reference the originating file** through `ast.Span.Start.Source`. After include flattening (per spec 001), spans still point at the original file.
- **Empty `Span` values** appear only when an `OptOut` kind has no source `NoneStmt` (which never happens: `OptOuts` is constructed only when a `NoneStmt` is encountered). All other span fields are required and never zero.
- **Stage values share AST pointers** — two `Resolved` values produced from the same `*ast.Program` will hold pointer-equal `src` fields. Two `Resolved` values produced from two `Parse` calls of equivalent source will hold pointer-distinct `src` fields, but the spans they expose compare equal by `(Path, Line, Column, Offset)`.
- **`Resolved` equality** is structural: two values are equal when handler counts match, each handler's stage list matches in kind+span+source-level, opt-outs match in kind+span, and error maps match in entry sequence.
