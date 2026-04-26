# 001 — DSL Parser Tasks

Tasks derived from the [plan](plan.md). Complete in order. Each task has a clear definition of done; do not move on until that condition is met.

## 1. Initialize the Go module

- [x] Create `go.mod` at the repository root with module path `github.com/stonean/writ` and Go 1.22 minimum.
- [x] Confirm `go build ./...` and `go vet ./...` succeed on an empty module.

**Done when:** the module exists, `go build ./...` is clean, and the repository can host Go packages.

## 2. Define source-position primitives

- [x] Create `ast/source.go` with `Source`, `Position`, and `Span` per the data model.
- [x] Implement `Span.Text()` returning the verbatim bytes the span covers.
- [x] Add a doc comment on the package (`ast`) declaring the AST is exported but unstable pre-1.0, per AGENTS.md.
- [x] Write unit tests for `Position` ordering and `Span.Text` on a synthetic `Source`.

**Done when:** the types compile, the package comment is in place, and the unit tests pass.

## 3. Define the AST node types

- [x] Create `ast/node.go` with the `Node` interface and `nodeBase`.
- [x] Create `ast/program.go` with `Program`, `SystemBlock`, `GroupBlock`, `HandlerBlock`, `ErrorsBlock`, `ErrorsEntry`, and `IncludeStmt` per the data model.
- [x] Create `ast/route.go` with `RoutePattern`, the `RouteSegment` interface, and the three segment types.
- [x] Create `ast/stmt.go` with the `Stmt` interface and one node type per pipeline statement listed in the data model.
- [x] Create `ast/expr.go` with `Call`, the `Expr` interface, every value-reference variant, the `Literal` interface, every literal type, the `NamedRef` helper, and the `ApproveExpr` tree.
- [x] Add the marker methods (`stmt()`, `expr()`, `literal()`, `routeSegment()`, `approveExpr()`) on each implementing type.
- [x] Run `go vet ./ast/...` and confirm a clean run.

**Done when:** every node type from the data model is declared, every interface is satisfied by its members, and `go build ./ast/...` is clean.

## 4. Implement the lexer

- [x] Create `parser/lexer.go` with `Token`, `TokenKind`, and a `lexer` struct that holds a `*ast.Source` and current position.
- [x] Implement scanning for: identifier (with dotted segments), integer (with optional leading minus), string (with `\"`, `\\`, `\n`, `\t`, `\r` escapes only), rate literal (integer + `/` + one of `sec`/`min`/`hour`/`day`), `->`, `(`, `)`, `,`, `:`, `=`, `*`, `/`, `-`, newline, EOF.
- [x] Strip line comments (`#` to end-of-line) so they never reach the parser.
- [x] Produce structured lex errors for unterminated strings, unknown escapes, raw newlines inside strings, bad rate units, and stray characters.
- [x] Track line and column accurately; advance line on `\n`, reset column at line boundaries.
- [x] Write a lexer-only test file `parser/lexer_test.go` covering each token kind, each error condition, and a multi-line input that exercises position tracking.

**Done when:** the lexer test suite passes and every error condition listed above produces a structured lex error with a precise span.

Note: integer with optional leading minus is split into a `MINUS` token followed by `INT`; the parser recombines at integer-expected positions. This avoids ambiguity with the `-` that appears inside route-segment literals (e.g., `m-search`) and HTTP method names. `RATE` recognition only fires inside parens (`parenDepth > 0`) so route patterns of any shape — including `/v1-2/things` or `/60/min` — tokenize as `SLASH`, `INT`/`IDENT`, and `MINUS` components rather than colliding with rate literals. (See task 6 for why paren-depth replaced the earlier previous-token rule.)

## 5. Implement the error type

- [x] Create `parser/error.go` with the `Error` struct (`File`, `Line`, `Column`, `Span`, `Message`) and an `Error() string` method that formats as `"file:line:col: message"`.
- [x] Confirm the type is comparable and printable in test failure output.

**Done when:** the type compiles and an `Error{}` value formats as expected in a unit test.

## 6. Implement the parser core (top-level + statements, no includes yet)

- [x] Create `parser/parser.go` with `Parse`, `ParseString`, `Option`, `WithFS`, `WithRoot`, and the `config` struct.
- [x] Implement recursive-descent functions: `parseProgram`, `parseSystemBlock`, `parseGroupBlock`, `parseHandlerBlock`, `parseErrorsBlock`, `parseStatement` (dispatching on the leading identifier lexeme), and one `parseXxxStmt` per pipeline statement kind.
- [x] Implement route-pattern parsing in a helper used by all three block types that take a pattern.
- [x] Implement call parsing (`parseCall`) and value-reference parsing (`parseExpr`) including the named-arg, body, query, route-param, and field-access forms.
- [x] Implement approve-expression parsing as a precedence-climbing tower (`parseApproveExpr`/`parseOrExpr`/`parseAndExpr`/`parseNotExpr`/`parsePrimaryExpr`).
- [x] Implement block-boundary detection: open block on `->` at end-of-line, close on next column-0 non-newline token or EOF.
- [x] Stub `parseIncludeStmt` to record an error "include not yet implemented" — the next task wires it.
- [x] Always return a non-nil `*ast.Program`.

**Done when:** every block kind, statement kind, value-reference kind, and the approve expression tower parse successfully on synthetic inputs with no includes.

Note: rate-vs-route disambiguation moved from previous-token tracking (task 4 note) to **paren-depth** tracking. `RATE` is only emitted inside parens (`parenDepth > 0`); outside parens the components emit as `INT SLASH IDENT` so route patterns and hyphenated literals like `/v1-2/things` parse cleanly. The previous-token approach failed on hyphenated route literals (`/v1-2/things` placed `MINUS` immediately before `INT(2)`, so the `SLASH`-only check let rate detection fire). Paren-depth is the structural signal for "we're inside a call argument list" — the only place rates legally appear.

## 7. Implement error recovery

- [x] Add a statement-level sync helper that consumes tokens to the next `NEWLINE` and continues with the next line.
- [x] Add a top-level sync helper that consumes tokens to the next column-0 keyword (`system`, `group`, `errors`, `include`, or `[A-Z][A-Z0-9-]*`).
- [x] Wire each `parseXxx` site that can fail to its appropriate sync helper.
- [x] Write a test where a single file contains three distinct syntax errors and assert that the parser reports all three in one pass.
- [x] Write a test where a malformed block header is followed by a valid block and assert that the valid block still parses.

**Done when:** the multi-error test passes and the recovery test confirms a partial AST contains the well-formed block.

## 8. Implement include resolution

- [x] Replace the `parseIncludeStmt` stub with real resolution: open the file via the configured `fs.FS`, lex/parse it, and inline its top-level constructs at the include point.
- [x] Resolve include paths relative to the *current* file's directory.
- [x] Maintain a cycle stack of open absolute paths; on cycle, emit a structured error naming the cycle and skip the include.
- [x] Reject include paths whose extension is not `.writ` (case-sensitive) with a parse error at the path span.
- [x] On a missing include file, emit a parse error at the `include` statement's span.
- [x] On a `system` block declared inside an included file, emit an error at the `system` block's span.
- [x] Ensure `Program.Sources` accumulates every file that contributed, in include-discovery order, with the root file at index 0.
- [x] Ensure all spans on inlined nodes still reference their originating `*Source`, not the root file.

**Done when:** the include acceptance tests pass: equivalent-to-single-file flattening, cycle reporting, missing-file reporting, system-in-include reporting, and `.writ`-extension enforcement.

Note: a `parseSession` struct now holds the cfg, cycle stack, source registry, and merged error list across recursive parser instances. `parseIncludeStmt` reads the path as a sequence of adjacent `IDENT/INT/SLASH/MINUS` tokens (not a single token) so subdirectory paths like `subdir/foo.writ` work. Cycle detection compares fsys-relative cleaned paths. Sources accumulate in include-discovery order with the root at index 0; all spans continue to reference their originating `*Source` because the sub-parser lexes against the included file directly.

## 9. Acceptance criterion test pass

Cover every checkbox under "Acceptance Criteria" in `spec.md`. Group tests by spec section. Use `parser.ParseString` with table-driven cases for the lexical and value-reference criteria; use `parser/testdata/*.writ` fixtures for include and multi-file criteria.

- [ ] **Constructs and Containment** — every checkbox: full-program parse, every keyword recognized contextually, identifier-with-keyword-segment, every uppercase-method case, lowercase-method rejected, empty-block rejected.
- [ ] **Lexical Forms** — every checkbox: identifier grammar, integer grammar, string escapes, rate units, comments, indentation permissiveness.
- [ ] **Value References and Calls** — distinct node kinds for each form, empty argument list.
- [ ] **Approve Expressions** — precedence and associativity table-driven, parenthesization.
- [ ] **Routes** — segment grammar, wildcard-only-final, empty-segment rejection, trailing-slash rejection, root-pattern accepted.
- [ ] **Multiple Format Lines and `none`** — ordered list preserved, `NoneStmt` is a distinct node kind.
- [ ] **Includes** — every checkbox: placement-independent flattening, cycle reporting, system-in-include rejection, missing-file reporting, `.writ`-extension enforcement.
- [ ] **Errors and Recovery** — every error carries `(file, line, column, message)`, multiple-errors-per-pass, AST always non-nil.
- [ ] **Source Locations and Determinism** — every node has a span starting and ending in its originating file, span text round-trips to the original bytes, parsing the same input twice produces structurally equal ASTs, no I/O beyond requested file reads.

**Done when:** every acceptance-criteria checkbox in `spec.md` has at least one corresponding passing test, and `go test ./parser/... ./ast/...` is green.

## 10. Determinism and "no I/O" guards

- [ ] Add a test that parses the same fixture twice and compares ASTs structurally (walking spans by `(Path, Line, Column, Offset)`).
- [ ] Add a test that runs `Parse` against an `fs.FS` that records every `Open` call, and assert that no file outside the include graph is opened (no environment reads, no working-directory walks).

**Done when:** both tests pass.

## 11. Markdown lint

- [ ] Run `npx markdownlint-cli2` on every `.md` file under `specs/001-dsl-parser/`. Fix any violations.

**Done when:** markdownlint exits clean on `spec.md`, `plan.md`, `data-model.md`, and `tasks.md`.

## 12. Status transition

- [ ] Confirm with the user that all acceptance criteria are testable as written, the data model is consistent with the spec, and the task ordering matches their judgment.
- [ ] Update `spec.md` status from `clarified` to `in-progress` only after task 1 begins. (The plan command transitions to `planned`; the implement command transitions to `in-progress`.)

**Done when:** the user has confirmed the plan and the spec status reads `planned`.
