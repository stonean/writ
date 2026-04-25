# 001 — DSL Parser Data Model

The parser produces an AST: a tree of typed Go values, each carrying its source span. This document defines that AST. The shapes here are the contract every downstream consumer (runtime, code generation, `writ show`, `writ routes`, LSP) reads.

All types live in package `github.com/stonean/writ/ast`. All exported types are documented as unstable pre-1.0 in their package comment.

## Source and Position

```go
// Source owns the original bytes of a single .writ file.
type Source struct {
    Path  string // file path as it would appear in error messages
    Bytes []byte // original file contents; never mutated
}

// Position points to a single rune-boundary inside a Source.
type Position struct {
    Source *Source
    Line   int // 1-based
    Column int // 1-based, byte-position within the line
    Offset int // 0-based byte offset into Source.Bytes
}

// Span is a half-open [Start, End) byte range inside a single Source.
// Start and End always reference the same Source.
type Span struct {
    Start Position
    End   Position
}

// Text returns the verbatim source bytes the span covers.
func (s Span) Text() []byte
```

`Position` and `Span` are values, not pointers. Equality is structural. `Span.Text()` is a thin convenience over `s.Start.Source.Bytes[s.Start.Offset:s.End.Offset]`.

## Node Interface

```go
// Node is the marker interface implemented by every AST node.
type Node interface {
    Span() Span
}

// nodeBase carries the span and is embedded by every concrete node.
type nodeBase struct{ span Span }
func (n nodeBase) Span() Span { return n.span }
```

Every concrete node embeds `nodeBase`. No node carries a parent pointer — walks are top-down via the `Program` root.

## Program (Root)

```go
// Program is the AST root. It is always non-nil after Parse, even on error.
type Program struct {
    nodeBase
    Sources  []*Source     // every file that contributed to this program, in include-discovery order
    System   *SystemBlock  // nil if no system block was declared
    Groups   []*GroupBlock
    Errors   []*ErrorsBlock
    Handlers []*HandlerBlock
}
```

Top-level constructs are kept in declaration order within their slice. `Sources[0]` is always the root file.

## Block-Level Nodes

```go
type SystemBlock struct {
    nodeBase
    Statements []Stmt
}

type GroupBlock struct {
    nodeBase
    Pattern    *RoutePattern
    Statements []Stmt
}

type HandlerBlock struct {
    nodeBase
    Method     string // verbatim method token (e.g., "GET", "MKCOL", "M-SEARCH")
    MethodSpan Span
    Pattern    *RoutePattern
    Statements []Stmt
}

type ErrorsBlock struct {
    nodeBase
    Pattern *RoutePattern
    Entries []*ErrorsEntry
}

type ErrorsEntry struct {
    nodeBase
    TypeName     string // verbatim, including the literal "default" for the catch-all
    TypeSpan     Span
    Formatter    string
    FormatterSpan Span
    IsDefault    bool // true when TypeName == "default"
}

type IncludeStmt struct {
    nodeBase
    Path     string // verbatim path as written, must end in ".writ"
    PathSpan Span
}
```

`IncludeStmt` is consumed by include resolution and does NOT appear in the final `Program` — its target's top-level constructs are inlined at the include point. The node type exists so error messages and partial-AST consumers can refer to the include site.

## Route Pattern

```go
type RoutePattern struct {
    nodeBase
    Segments []RouteSegment // empty for the root pattern "/"
}

// RouteSegment is one of: literal, parameter, wildcard.
type RouteSegment interface {
    Node
    routeSegment()
}

type LiteralSegment   struct { nodeBase; Text string }       // e.g., "users"
type ParameterSegment struct { nodeBase; Name string }       // e.g., "id" for ":id"
type WildcardSegment  struct { nodeBase }                     // "*", only valid as the final segment
```

The parser enforces that `WildcardSegment` only appears as the final element. Other route validations (parameter-name uniqueness, leading slash, etc.) are parser-level too because they are syntactic.

## Pipeline Statements

```go
// Stmt is the marker interface for every pipeline statement.
type Stmt interface {
    Node
    stmt()
}

type LogStmt      struct { nodeBase; Args []Expr }
type MeasureStmt  struct { nodeBase; Args []Expr }
type SessionStmt  struct { nodeBase; Storage string; StorageSpan Span }
type CSRFStmt     struct { nodeBase; Mode string; ModeSpan Span }
type LimitStmt    struct { nodeBase; Call *Call }
type ApproveStmt  struct { nodeBase; Expr ApproveExpr }
type ResolveStmt  struct { nodeBase; Name string; NameSpan Span; Call *Call }

type CommitStmt struct {
    nodeBase
    Name     string // empty for fire-and-forget (e.g., commit db.users.delete(:id))
    NameSpan Span   // zero Span when Name == ""
    Call     *Call
}

type EmitStmt struct {
    nodeBase
    Event     string
    EventSpan Span
    Data      string // empty when no "with <data-name>" clause
    DataSpan  Span
}

type FormatStmt struct {
    nodeBase
    Template     string
    TemplateSpan Span
    Data         []NamedRef // arguments after "with"
    Layout       string     // empty when no "using layout <name>" clause
    LayoutSpan   Span
}

type RedirectStmt struct {
    nodeBase
    URL     string // template literal as written
    URLSpan Span
}

type LayoutStmt struct {
    nodeBase
    Name     string
    NameSpan Span
}

// NoneStmt is "<stage> none" — explicit opt-out, distinct from "stage not declared".
type NoneStmt struct {
    nodeBase
    Stage     string // verbatim stage keyword: "log", "session", "csrf", "limit", "approve", "format", etc.
    StageSpan Span
}
```

`NoneStmt` deliberately exists as its own type rather than an `Empty` flag on each stage type — the runtime distinguishes "explicitly opted out" from "inherited" by node *kind*, not a boolean.

## Calls and Value References

```go
type Call struct {
    nodeBase
    Name     string // dotted identifier, e.g., "db.users.create", "auth.isOwner"
    NameSpan Span
    Args     []Expr // empty slice when "()"
}

// Expr is anything that can appear in a call argument list or as a "with" data reference.
type Expr interface {
    Node
    expr()
}

// Value-reference forms — each a distinct node kind per the spec.
type RouteParamRef struct {
    nodeBase
    Name     string // identifier following ":"
    NameSpan Span
}

type FieldRef struct {
    nodeBase
    Root      string   // first segment after ":" (e.g., "user" in ":user.id")
    RootSpan  Span
    Path      []string // subsequent dotted segments (at least one for a FieldRef vs. RouteParamRef)
    PathSpans []Span
}

type NamedArg struct {
    nodeBase
    Name      string
    NameSpan  Span
    Value     Literal // currently IntLit, StringLit, or RateLit
}

type BodyRef  struct { nodeBase; TypeName string; TypeSpan Span }
type QueryRef struct { nodeBase; TypeName string; TypeSpan Span }

// Literal forms used as standalone arguments and as NamedArg.Value.
type Literal interface {
    Expr
    literal()
}

type IntLit    struct { nodeBase; Value int64 }
type StringLit struct { nodeBase; Value string } // escapes already processed
type RateLit   struct {
    nodeBase
    Count int64
    Unit  string // one of "sec", "min", "hour", "day"
}
```

`NamedRef` is a thin wrapper used by `FormatStmt.Data`:

```go
// NamedRef is one entry in a "with <data-list>" clause — either a bare name or a field reference.
type NamedRef struct {
    nodeBase
    Name      string   // bare identifier, or "" if Path is set
    Path      []string // for nested references, e.g., "user.profile" -> ["user", "profile"]
    PathSpans []Span
}
```

## Approve Expression Tree

```go
// ApproveExpr is the boolean-expression tree built from "approve" statements.
type ApproveExpr interface {
    Node
    approveExpr()
}

type ApproveOr   struct { nodeBase; Left, Right ApproveExpr }
type ApproveAnd  struct { nodeBase; Left, Right ApproveExpr }
type ApproveNot  struct { nodeBase; Inner ApproveExpr }

// ApproveCall is a leaf — one approver invocation, e.g., "auth.isOwner(:id)".
type ApproveCall struct {
    nodeBase
    Call *Call
}
```

The tree shape encodes the spec's precedence (`NOT > AND > OR`) and associativity (`NOT` right; `AND`/`OR` left). Parentheses do not appear in the tree — they are flattened by the precedence-climbing parser. The tree shape itself is the record of parenthesization.

## Notes

- **All `Span` fields reference the originating file**, not the post-flatten position in the root file. Includes preserve original locations per the spec's *Source Locations* contract.
- **Empty `Span` values** (zero `Position`s) appear only on optional fields when the corresponding source construct was absent (e.g., `CommitStmt.NameSpan` for fire-and-forget commits). Required spans are never zero.
- **No node carries comments.** Line comments are stripped at lex time. Tools that need to render or preserve comments operate on the lexer directly (a future LSP concern, not the parser's contract).
- **AST equality** is structural — comparing two ASTs from two `Parse` calls of the same input must be value-equal modulo `*Source` pointer identity (the bytes content is equal). The determinism acceptance test compares spans by `(Path, Line, Column, Offset)` rather than pointer.
