# 001 — DSL Parser Plan

## Overview

The parser is a hand-written, single-pass, recursive-descent parser over a hand-written lexer. It produces an in-memory AST (a tree of typed nodes, each carrying a source span) and a flat list of structured errors. Includes are resolved during the same pass via a recursion-with-cycle-stack technique. The parser is a pure Go library: no logging, no global state, no I/O beyond reading the files it is asked to read.

The implementation lives in two sibling packages — `ast` (exported AST node types and source-position primitives) and `parser` (exported `Parse` / `ParseString` entry points, lexer, and error type). Both are exported but documented as unstable pre-1.0, per AGENTS.md.

## Technical Decisions

### Hand-written recursive-descent parser

A hand-rolled recursive-descent parser is the right choice for this grammar. The DSL has only two structural levels (column-0 constructs and indented pipeline statements), no expression precedence beyond the `approve` boolean expression, and no left recursion. Hand-rolling gives precise error messages, full control over recovery, and a zero-dependency build path that matches the spec's "no magic" tone.

Alternatives considered:

- **goyacc** — automatic LALR(1) generation, but generated parsers historically produce poor error messages and are awkward to recover from. Overkill for a two-level grammar.
- **participle / pigeon** — adds an external runtime dependency for a parser the team will own forever. Less control over per-token error positioning.
- **ANTLR + Go target** — heavy toolchain, codegen step in the build, runtime dependency. Disproportionate for the scope.

### Hand-written lexer

The lexer is a byte-oriented scanner that emits a stream of tokens, each with a `Span` (start and end position). It tracks line and column as it advances, increments line on `\n`, and resets column at line boundaries. Tabs count as one column; the parser does not interpret indentation width (per spec), so columns are simply byte-position-within-line.

The lexer emits one token kind per lexical form (identifier, integer, string, rate, `->`, `(`, `)`, `,`, `:`, `=`, `*`, `/`, `-`, newline, EOF) plus a sentinel for lex errors (unterminated string, unknown escape, bad rate unit). Reserved words (`system`, `group`, `with`, `OR`, etc.) are NOT distinguished at lex time — they emerge as ordinary identifiers and the parser checks the lexeme at each grammar position. This is what the spec calls "contextual keywords" and is trivial to implement when the lexer hands the parser the original bytes.

The lexer emits a synthetic `NEWLINE` token at the end of each logical line. The parser uses `NEWLINE` plus the *column-of-next-non-newline-token* to decide block boundaries: a non-newline token at column 0 closes any open block and begins a new top-level construct; a non-newline token at column > 0 continues the current block.

### Source-byte ownership and Spans

Every AST node carries a `Span` (start `Position`, end `Position`). A `Position` is `{Source *Source, Line, Column, Offset int}` where `Source` points to the file the token was read from. A `Source` value owns the original byte slice and the file path. Consumers extract verbatim text via `source.Bytes(span)` — no node duplicates source text into its own field.

The `Program` root keeps a registry (`Sources []*Source`) of every file that contributed to the parse, so consumers can walk a fully-resolved program without re-reading the disk.

### Include resolution and filesystem boundary

The parser resolves includes during the same pass that builds the AST. When the parser encounters `include "path.writ"` at column 0:

1. Resolve the path relative to the *current* file's directory (not the root file's).
2. Check the cycle stack — if the resolved absolute path is already open, emit a structured cycle error with both the `include` span and the chain of files that led here.
3. Open the file via the configured filesystem, lex/parse it, and inline its top-level constructs at the include point.
4. Pop the cycle stack on return.

The filesystem is abstracted behind `io/fs.FS`. `Parse(rootPath)` defaults to `os.DirFS` rooted at `filepath.Dir(rootPath)`. Tests and the `writ` LSP can inject an in-memory `fs.FS`. `ParseString(virtualPath, source)` is a convenience wrapper that wires an in-memory FS containing one file.

### Error type and accumulation

`parser.Error` carries `File string`, `Line, Column int`, `Span ast.Span`, `Message string`. The parser returns `[]parser.Error`; the AST root is always non-nil. Strict consumers (runtime, code generator) treat `len(errs) == 0` as the authoritative success indicator.

Errors are accumulated in a single slice. The parser does not distinguish "lex error" from "parse error" in the public type — the consumer sees one uniform list ordered by source position.

### Error recovery strategy

Two synchronization points, both used by the spec's "report multiple errors per pass" criterion:

- **Statement-level sync** — inside a block, on a malformed statement, skip the rest of the line (consume tokens until the next `NEWLINE`), record the error, continue with the next line.
- **Top-level sync** — on a malformed block header (or a block header with no statements after a parsed body fails badly), skip tokens until the next column-0 keyword (`system`, `group`, `errors`, `include`, or any uppercase ASCII identifier matching `[A-Z][A-Z0-9-]*`). Resume parsing from there.

Recovery is deterministic — no randomness, no token-budget heuristics. The implementation is one helper function per sync point, called from each `parseXxx` site that needs to recover.

### Approve expression precedence

A precedence-climbing function tower implements `NOT > AND > OR` with right-associative `NOT` and left-associative `AND`/`OR`:

```text
parseApproveExpr      = parseOrExpr
parseOrExpr           = parseAndExpr (`OR`  parseAndExpr)*
parseAndExpr          = parseNotExpr (`AND` parseNotExpr)*
parseNotExpr          = `NOT` parseNotExpr | parsePrimaryExpr
parsePrimaryExpr      = `(` parseOrExpr `)` | parseCall
```

All four reserved words (`OR`, `AND`, `NOT`, plus the no-op-at-this-level `with`) are checked by lexeme against the current identifier token.

### Public API

```go
package parser

func Parse(rootPath string, opts ...Option) (*ast.Program, []Error)
func ParseString(virtualPath, source string) (*ast.Program, []Error)

type Option func(*config)
func WithFS(fsys fs.FS) Option   // override filesystem for include resolution
func WithRoot(dir string) Option // override include-resolution root

type Error struct {
    File    string
    Line    int
    Column  int
    Span    ast.Span
    Message string
}
func (e Error) Error() string
```

Refinements from the spec sketch:

- `errs` is `[]parser.Error`, not `error`. The spec sketch's `errs` field name is preserved.
- Options collected as functional options so future extensions (e.g., `WithMaxIncludes`) don't break callers.
- `ParseString` takes a virtual path so error messages report a meaningful filename for in-memory sources.

### Determinism

No maps are iterated in source-order-sensitive contexts. The source registry is a slice indexed by include order. The cycle stack is a slice of `*Source`. No time-, env-, or randomness-dependent code paths exist. The acceptance test suite includes a "parse twice, AST equal" check.

### Package layout

Per AGENTS.md, the AST package is exported and sibling to the parser package:

```text
github.com/stonean/writ/
  go.mod
  ast/
    source.go      # Source, Position, Span
    node.go        # Node interface, common helpers
    program.go     # Program + block-level node kinds
    route.go       # RoutePattern + RouteSegment
    stmt.go        # Pipeline statement node kinds
    expr.go        # Call, ValueRef variants, ApproveExpr tree
  parser/
    parser.go      # Parse, ParseString, Option, recursive-descent body
    lexer.go       # Lexer + Token + TokenKind
    error.go       # Error type
    parser_test.go # Acceptance tests
    testdata/      # Fixture .writ files
      includes/    # Cycle, missing, valid, .txt-extension fixtures
```

Both packages are documented as unstable pre-1.0 in their package comment, per AGENTS.md spec convention.

## Affected Files

| File | Action | Purpose |
| --- | --- | --- |
| `go.mod` | Create | Module path `github.com/stonean/writ`, Go 1.22 minimum |
| `ast/source.go` | Create | `Source`, `Position`, `Span` types and helpers |
| `ast/node.go` | Create | `Node` interface, shared `nodeBase`, walking helpers |
| `ast/program.go` | Create | `Program`, `SystemBlock`, `GroupBlock`, `ErrorsBlock`, `HandlerBlock`, `IncludeStmt`, `ErrorsEntry` |
| `ast/route.go` | Create | `RoutePattern`, `RouteSegment` (literal / parameter / wildcard variants) |
| `ast/stmt.go` | Create | `Stmt` interface plus one node per pipeline statement (`LogStmt`, `MeasureStmt`, `SessionStmt`, `CSRFStmt`, `LimitStmt`, `ApproveStmt`, `ResolveStmt`, `CommitStmt`, `EmitStmt`, `FormatStmt`, `RedirectStmt`, `LayoutStmt`, `NoneStmt`) |
| `ast/expr.go` | Create | `Call`, value-reference variants (`RouteParamRef`, `FieldRef`, `NamedArg`, `BodyRef`, `QueryRef`), literal nodes (`IntLit`, `StringLit`, `RateLit`), and approve-expression tree (`ApproveAnd`, `ApproveOr`, `ApproveNot`, `ApproveCall`) |
| `parser/parser.go` | Create | `Parse`, `ParseString`, `Option`, recursive-descent body, recovery helpers |
| `parser/lexer.go` | Create | Hand-written lexer, `Token`, `TokenKind` |
| `parser/error.go` | Create | `Error` type |
| `parser/parser_test.go` | Create | Acceptance criterion tests, including the "parse twice, equal" determinism test |
| `parser/testdata/` | Create | `.writ` fixture files for include-resolution, cycle, and acceptance tests |

The `data-model.md` companion document defines the full AST node shape.

## Trade-offs

### Considered and rejected

- **Lex-time keyword recognition** — would let the parser switch on `TokenKind == KW_SYSTEM` instead of comparing strings. Rejected because the spec mandates contextual keywords (`db.session.refresh` must parse as an identifier even though `session` is a statement keyword); a separate keyword token kind would either prohibit such names or require the lexer to know its grammar context, which defeats the purpose of having a separate lexer.
- **Single AST package containing everything** — considered keeping `ast` as one file. Rejected because the spec's six logical groups (program structure, routes, statements, expressions, source positions, base node types) split cleanly into six small files that are easier to scan than one ~600-line file.
- **Separate `parser/lexer` subpackage** — would isolate the lexer behind a smaller surface. Rejected because the lexer has no consumers outside the parser; an internal package would mean exporting types for the parser to import them. Keeping the lexer as a non-exported file inside `parser/` is the standard Go layout for hand-written parsers (see `go/parser`, `text/template/parse`).
- **Returning `error` instead of `[]Error`** — would let callers use `errors.Is`/`errors.As`. Rejected because the spec explicitly calls for "more than one error in a single pass" — a flat slice makes that the obvious shape, and a wrapping `error` would force consumers to type-assert to get the slice anyway. The runtime gates on `len(errs) == 0`; the slice form is more honest.
- **AST nodes as concrete structs vs. an interface** — settled on small struct types per node kind, all implementing a marker `Node` interface. Type switches at consumer sites are idiomatic Go for this shape (mirrors `go/ast`). Avoids the visitor-pattern boilerplate that would only pay off with a much larger node count.
- **Storing raw lexeme on each AST node** — would simplify some downstream tools at the cost of doubling AST memory. Rejected per the spec's "Source preservation" decision: spans plus addressable source bytes give the same answer on demand.

### Known limitations

- **Stack depth on deeply nested includes** — the include resolution is a normal recursive call. A pathological include chain (e.g., 10,000 deep) will hit Go's default goroutine stack growth (which grows dynamically up to 1 GiB by default, then panics). Per the spec's "Maximum nesting / file size" decision, this is acceptable — the parser is a build-time tool reading the developer's own files.
- **No incremental reparse** — every `Parse` call re-reads and re-parses every file. An LSP that needs incremental updates will wrap the parser, not bypass it. Adding incremental support is additive when the LSP spec lands.
- **No source-map back-reference for include flattening beyond `Span.Source`** — the AST records the original file via `Span.Source`, which is sufficient for "where was this written?". A consumer that needs the chain of includes that led to the current file can walk the source registry, but the parser does not pre-compute that chain. If an LSP later needs a "who included me" query, that's an additive method on `Source`.
- **No structured warnings** — the parser only produces errors. Warnings (e.g., "unrecognized HTTP method", per the spec's HTTP-method extensibility decision) belong to startup validation, not the parser.

## Open Questions Resolved

All open questions in the spec are recorded as resolved. This plan does not introduce new questions.
