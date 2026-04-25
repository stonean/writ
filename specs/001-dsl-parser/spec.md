# 001 — DSL Parser

**Status:** planned
**Dependencies:** none

The DSL parser turns one or more `.writ` files into a single in-memory representation — an Abstract Syntax Tree (AST) — that downstream features (the pipeline runtime, code generation, the `writ show` and `writ routes` CLIs) consume. The parser is purely syntactic: it recognizes the constructs documented in the README's *DSL Syntax* section, flattens includes, and reports structured errors. It does no semantic checking — name registration, field references, dependency cycles, and pairing rules are all the responsibility of later stages.

The parser is the foundation every other Writ feature stands on. A change to its output breaks every consumer, so its contract — what it accepts as input, what shape it produces, what errors it reports — needs to be pinned down explicitly before any consumer is built.

## Inputs

The parser accepts:

- A path to a root `.writ` file (typically `app.writ`).
- A resolution root (a base directory) used to locate `include`d files. By default, the directory of the root file.

## Outputs

The parser produces one of two outcomes:

- **Success** — a single AST representing the entire application's pipeline configuration, with all `include`d files inlined.
- **Failure** — a structured collection of parse errors, each carrying the file path, line, column, and a human-readable message. The parser does not abort on the first error; it collects as many as it can usefully report in one pass.

Either outcome is a value — the parser does not write to stdout, log, or exit the process.

## Recognized DSL Surface

The parser recognizes every construct documented in the README's *DSL Syntax* section. This list is the public contract:

### Block Headers

- `system ->` — root-level block, defines pipeline defaults.
- `group <route-pattern> ->` — block scoped to a route prefix (e.g., `/admin/*`).
- `errors <route-pattern> ->` — block mapping error type names to formatter names within a route scope.
- `<METHOD> <route> ->` — handler block for a specific HTTP verb and route. Methods include at least `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS` (see Open Questions).

### Pipeline Statements

These appear inside `system`, `group`, or handler blocks (subject to where each is meaningful — the parser accepts them positionally; semantic restrictions belong to the runtime):

- `log <args>`
- `measure <args>`
- `session <storage>`
- `csrf <mode>`
- `limit <call>`
- `approve <expression>` — supports `OR`, `AND`, `NOT` composition with parentheses
- `resolve <name> = <call>`
- `commit [<name> =] <call>` — name is optional for fire-and-forget commits like `commit db.users.delete(:id)`
- `emit <event-name> [with <data-name>]`
- `format <template> with <data-list> [using layout <name>]`
- `redirect <url-template>`
- `layout <name>`
- `<stage> none` — explicit opt-out of an inherited stage

### Top-Level Statements

- `include <path>` — inline another `.writ` file at this point in the document.

### Errors-Block Entries

Inside an `errors <pattern> ->` block, each line is `<TypeName> <formatter-name>` or `default <formatter-name>`.

### Calls

A call is `<name>(<args>)`, where:

- `<name>` is a dotted identifier (e.g., `db.users.create`, `auth.isOwner`, `rate.ip`).
- `<args>` is a comma-separated list of value references (see below). Empty `()` is allowed.

### Value References

The parser recognizes these argument forms, each as a distinct AST node kind:

- `:<identifier>` — route parameter (e.g., `:id`).
- `:<identifier>.<identifier>[.<identifier>...]` — field access on a previous resolve/commit result (e.g., `:user.id`, `:user.team_id`).
- `<identifier>=<literal>` — static named argument (e.g., `limit=10`, `status="active"`).
- `body <TypeName>` — typed request body.
- `query <TypeName>` — typed query parameters.

Literals include integers, strings (double-quoted), and the rate-syntax form `<n>/<unit>` (e.g., `60/min`).

### Comments

`#` begins a line comment that runs to end-of-line. (Block comment syntax is an open question.)

## Include Resolution

`include <path>` causes the named file to be parsed and its top-level constructs inlined at the point of the `include` statement, in declaration order. After resolution, the AST is indistinguishable from one produced by an equivalent single file.

Constraints:

- Paths are resolved relative to the file containing the `include`, not the root file.
- Include cycles are detected and reported as a structured error (not a stack overflow).
- A `system` block in an included file is an error — the system block lives in the root file only.
- A missing include file is an error.

## Source Locations

Every AST node carries its source location: file path, line, and column. This is non-negotiable — every downstream consumer (runtime errors, `writ show`, code generation diagnostics, IDE tooling) depends on being able to point back to the source.

After include flattening, the recorded location is the original file the construct was written in, not the post-flatten position in the root file.

## Idempotency

Parsing the same input twice produces equivalent ASTs. The parser has no mutable global state, no time- or environment-dependent behavior, and no I/O beyond reading the files it is asked to read.

## Out of Scope

The parser is purely syntactic. The following are **not** the parser's responsibility — each belongs to a later feature:

- Verifying that resolver, approver, formatter, limiter, error-handler, layout, template, body-type, query-type, error-type, or event names are registered (→ startup validation).
- Verifying that route parameters referenced in calls (`:id`) actually appear in the route definition (→ startup validation).
- Verifying that field references (`:user.id`) match the return type of the referenced resolve/commit (→ code generation + startup validation).
- Verifying SQL parameter and column names against struct fields (→ code generation + data layer).
- Pairing rules: `csrf auto` requires `session`; `using layout` requires an `.html` format; a handler must end in `format`/`redirect`; `format` and `redirect` cannot be mixed except for content negotiation (→ startup validation).
- Resolving overrides (handler beats group beats system) and computing each handler's effective pipeline (→ pipeline runtime).
- Detecting resolve/commit dependency cycles (→ pipeline runtime or startup validation).
- Reading or matching `.sql` files in `queries/` (→ data layer).
- Loading or rendering templates (→ HTML rendering).

The parser exposes structure. Other features assign meaning.

## Public Go API (Contract Sketch)

The parser is a Go library consumed by the runtime, code generator, and CLI. Per project convention (see `AGENTS.md` *Spec Conventions*), the intended Go-side shape is sketched here as a contract illustration; exact spellings are refined in the plan phase.

A consumer parses a root file:

```go
ast, errs := parser.Parse("app.writ")
```

`errs` is empty on success. On failure, `ast` may still be partial (best-effort) so that tooling can report on whatever parsed.

The AST is walkable — a consumer can iterate the system block, groups, handlers, and errors blocks, and for each can iterate its pipeline statements, retrieving each statement's stage kind, arguments (typed by reference kind), and source location.

Test helpers parse from in-memory strings to keep parser tests independent of the filesystem:

```go
ast, errs := parser.ParseString("app.writ", source)
```

These spellings are illustrative. Plan phase resolves the exact package, type, and function shapes.

## Acceptance Criteria

### Constructs and Containment

- [ ] Parsing a `.writ` file containing a `system` block, multiple `group` blocks, multiple handler blocks, and one or more `errors` blocks produces an AST that contains a node for each construct.
- [ ] All keywords in the README's *DSL Syntax* section are recognized as contextual keywords: `system`, `group`, `errors`, `include`, `log`, `measure`, `session`, `csrf`, `limit`, `approve`, `resolve`, `commit`, `emit`, `format`, `redirect`, `layout`, `none`, `OR`, `AND`, `NOT`, `with`, `using`, `body`, `query`, and `default`.
- [ ] An identifier whose segments include a keyword word (e.g., `db.session.refresh`, `auth.with.something`) parses as a normal identifier in any identifier position.
- [ ] Any uppercase ASCII identifier matching `[A-Z][A-Z0-9-]*` is accepted as a handler-block-header method, including `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, and non-standard methods like `MKCOL`, `PROPFIND`, `PURGE`, `M-SEARCH`.
- [ ] A lowercase method (e.g., `get`) at handler-block-header position is a parse error.
- [ ] An empty block (a block header followed by no statements, ignoring blank and comment-only lines) is a parse error that names the block and its location.

### Lexical Forms

- [ ] Identifiers match `[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)*` — ASCII only, segment-leading letter, no consecutive/leading/trailing dots.
- [ ] Decimal integer literals match `-?[0-9]+`. No underscores, hex, octal, scientific notation, or floats.
- [ ] String literals are double-quoted; supported escapes are `\"`, `\\`, `\n`, `\t`, `\r`, and no others. Raw newlines inside strings, unterminated strings, and unknown escape sequences are parse errors.
- [ ] Rate literals match `<integer>/<unit>` where `<unit>` is one of `sec`, `min`, `hour`, `day`. Other unit names are parse errors.
- [ ] Line comments begin with `#` and run to end-of-line. They are stripped from the parsed output and do not appear in the AST. There is no block-comment form.
- [ ] Indentation: any line that begins with leading whitespace (tab, space, or mix) is a continuation of the open block; a line that begins at column 0 is a new top-level construct or terminates the open block. The parser does not validate indentation character or width.

### Value References and Calls

- [ ] Each value reference form parses to a distinct AST node kind: route parameter (`:id`), field access (`:user.id`, including multi-level `:user.team.id`), static named arg (`limit=10`, `status="active"`), typed body (`body CreateUserInput`), typed query (`query ListUsersQuery`).
- [ ] A call argument list may be empty (`db.users.list()` is valid).

### Approve Expressions

- [ ] An `approve` expression with `OR`, `AND`, `NOT`, and parentheses parses with `NOT > AND > OR` precedence; `NOT` is right-associative; `AND` and `OR` are left-associative.
- [ ] Parentheses in an `approve` expression override the default precedence and are accepted at any level of nesting.

### Routes

- [ ] Route patterns parse as `/` followed by zero or more `/`-separated segments. Each segment is a literal (`[a-zA-Z0-9_-]+`), a parameter (`:` + literal), or a wildcard (`*`).
- [ ] A wildcard segment is only valid as the final segment; `/users/*/posts` is a parse error.
- [ ] Empty segments (`//users`) and trailing slashes (`/users/`) are parse errors. The root pattern `/` is valid.

### Multiple Format Lines and `none`

- [ ] Multiple `format` lines in a single handler parse as an ordered list (for content negotiation), not as a duplicate-key error.
- [ ] An explicit `<stage> none` is preserved in the AST as a distinct node from "stage not declared," so the runtime can distinguish opt-out from inheritance.

### Includes

- [ ] An `include` statement is valid at any column-0 position in the root file; placement does not affect the resulting AST beyond document order.
- [ ] Parsing a root file that `include`s another file produces an AST equivalent to one parsed from a single file with the included contents inlined at the include point.
- [ ] `include` paths must end in `.writ` (case-sensitive); other extensions are a parse error.
- [ ] An include cycle (file A includes file B includes file A) is reported as a structured error with file/line/column, not as a stack overflow.
- [ ] A `system` block declared in an included file is reported as an error with location.
- [ ] A missing include file is reported as an error with the location of the `include` statement.

### Errors and Recovery

- [ ] Every parse error carries a file path, line number, column number, and a human-readable message.
- [ ] When a single file contains multiple syntax errors, the parser reports more than one in a single pass (it does not abort on the first).
- [ ] The parser always returns a non-nil AST root; on failure, the AST contains every construct that parsed successfully, and the returned error list is the authoritative success indicator.

### Source Locations and Determinism

- [ ] Every AST node carries the source location *range* it spans — starting `(file, line, column)` to ending `(file, line, column)` — referencing the original file the construct was written in, not the post-flatten position in the root file.
- [ ] The parser keeps original source bytes addressable so consumers can extract the verbatim source text for any node via its span. The parser does not duplicate raw source text into AST nodes.
- [ ] Parsing the same input twice produces equivalent ASTs (no time-, environment-, or order-dependent variation).
- [ ] The parser performs no I/O beyond reading the files it is given (no network, no environment reads, no logging).

## Open Questions

*All open questions resolved.*

## Resolved Questions

- **Indentation sensitivity & block termination** — The grammar has only two levels: top-level constructs (`system`, `group`, `errors`, handler blocks, `include`) and the pipeline statements inside them. A block opens with `->` at end-of-line and consists of all subsequent lines that begin with any leading whitespace (tab, space, or mix — the parser does not validate which). The block ends when a line begins at column 0 (the next top-level construct) or at end-of-file. Blank lines and comment-only lines are ignored and do not terminate a block. Style enforcement (canonical indent character and width) is the responsibility of a future `writ fmt` tool, not the parser. Rationale: matches Go's model — language is permissive, formatter is opinionated. Keeps the parser maximally simple and avoids tab-vs-space disputes at parse time.
- **Block comments** — Not supported. Line comments (`#` to end-of-line) are the only comment form. Multi-line headers are written as consecutive `#` lines, which `writ fmt` will preserve as a group. Rationale: keeps the lexer minimal, avoids the unterminated-comment-at-EOF and comment-inside-string edge cases, and matches the precedent of small terse-syntax languages (TOML, YAML). Idiomatic Go almost never reaches for `/* */` either.
- **Approve expression precedence** — Standard logical precedence: `NOT` (unary, right-associative) > `AND` (left-associative) > `OR` (left-associative). So `NOT a AND b OR c` parses as `((NOT a) AND b) OR c`. Parentheses are accepted at any point and override the default precedence; they are never required. Rationale: matches every mainstream language (SQL, Python, Go, JS) — principle of least surprise. `writ fmt` may choose to add explicit parentheses for clarity, but that is a formatter policy, not a parser rule.
- **Route pattern grammar** — Minimal: a pattern is `/` followed by zero or more segments separated by `/`. A segment is a literal (`[a-zA-Z0-9_-]+`), a parameter (`:` + literal), or a wildcard (`*`, only valid as the final segment). No regex, no optional segments, no empty segments, no trailing slash. The root path `/` is valid. Rationale: covers every README example and every common Go router pattern; avoids the bug surface of regex routes and the readability cost of optional segments. Future extensions (regex, optionals) are additive and won't break existing patterns. Scope note: the parser only validates pattern *shape* and captures the parameter names; pattern *matching* belongs to the runtime.
- **HTTP method extensibility** — Any uppercase ASCII identifier matching `[A-Z][A-Z0-9-]*` is accepted at the handler-block-header position. So `GET`, `POST`, `MKCOL`, `PROPFIND`, `M-SEARCH`, and any future or custom method all parse. Lowercase is rejected (HTTP methods are case-sensitive per RFC 9110). Rationale: matches Go's `http.NewRequest` permissiveness, supports WebDAV / CalDAV / custom methods like `PURGE` without a Writ release, and avoids a maintenance burden of tracking RFCs. Scope note: the parser does not validate that a method is well-known; if startup validation later wants to warn on unrecognized methods, it can.
- **String literal syntax** — Double-quoted only (`"..."`). No single-quoted form, no raw-string (backtick) form. Supported escapes: `\"`, `\\`, `\n`, `\t`, `\r` — that is the complete set. Strings may not contain raw newlines (use `\n`). Unterminated strings and unknown escape sequences are parse errors. Rationale: static DSL args are short, simple values; one quoting style removes a needless decision; a tiny escape set keeps the lexer trivial and catches typos like `\q` instead of silently passing them through. Identifiers (`db.users.create`) are not strings and do not need quoting — strings are only for opaque values.
- **Numeric literal forms** — Decimal integers only, with optional leading minus: `-?[0-9]+`. No underscores, no hex, no octal, no scientific notation, no floats. Rationale: every numeric value in the README is a plain integer; the DSL is for wiring, not arithmetic. Anything needing richer numeric expressiveness belongs in Go-side configuration. Future extensions (underscores, hex, floats) are additive and won't break existing literals.
- **Rate literal grammar** — `<integer>/<unit>` where `<unit>` is exactly one of `sec`, `min`, `hour`, `day`. Integer count only (consistent with the numeric-literal decision). One canonical name per unit — no synonyms (no `s`, `m`, `h`, `second`, `minute`, etc.) — because `m` is ambiguous between minute and month, and synonyms add lexer surface for no real benefit. Sub-second granularity is expressed as a higher per-second count (e.g., `1000/sec` instead of `1/ms`); fractional rates are expressed at the next-finer unit (e.g., `30/min` instead of `0.5/sec`). Scope note: the parser produces a structured rate node carrying count and unit; converting to a duration and applying the rate is the limiter's job.
- **Identifier rules** — Grammar: `[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)*`. ASCII only. Each dotted segment must start with a letter; remaining characters are letters, digits, or underscores. Case is preserved and significant. Dashes are not allowed. Consecutive dots, leading dots, and trailing dots are not valid. Rationale: identifiers ultimately bind to Go symbols, which keeps the character set ASCII-friendly; dashes conflict with subtraction; Unicode adds tooling friction (terminals, grep) for vanishingly rare benefit. The naming conventions visible in the README (lowercased dotted for callable names, PascalCase for Go type references in `body`/`query` positions) are conventions enforced by `writ fmt` if at all, not by the parser — the parser accepts any well-formed identifier in any identifier position.
- **File extension enforcement** — `include` paths must end in `.writ` (case-sensitive). A path that does not is a parse error with a clear message. Rationale: enforces a consistent file convention for editors, formatters, dev-mode watchers, and grep; prevents ambiguity (`include admin` could otherwise mean `admin`, `admin.writ`, or `admin/index.writ`); catches typos at parse time instead of file-not-found time. The parser does not enforce that the *root* file passed in ends in `.writ` — that's the caller's choice; only `include` paths within DSL source are policed.
- **Maximum nesting / file size** — No hard limits enforced by the parser. Include cycles are already detected (per the *Include Resolution* contract); beyond that, the parser is bounded only by Go's natural memory and stack limits. Rationale: the parser is a build-time / startup-time tool that only ever reads files from the developer's own project — there is no untrusted-input vector. Arbitrary numeric limits age badly and the only person hurt by a 10,000-deep include chain is the developer who wrote it. A pathological input surfaces as a clear OOM or stack overflow, not as silent corruption.
- **Partial AST on failure** — The parser always returns a non-nil AST root, even when errors are present. The AST contains every construct that parsed successfully (the parser uses error recovery: synchronize to the next top-level keyword at column 0 after a broken block header, or to the next line inside a block). Errors are returned alongside. Strict consumers (runtime, code generation) treat `len(errs) == 0` as the authoritative success indicator and refuse to proceed otherwise. Tooling consumers (LSP, `writ show`, `writ routes`) can still render the parts that parsed. Rationale: cost is bounded (standard recursive-descent recovery), benefit is large (a single typo on line 47 should not blank out IDE features for the rest of the file).
- **Source preservation** — Every AST node carries the source location range it spans: starting `(file, line, column)` to ending `(file, line, column)`. The parser keeps original source bytes addressable so that consumers can extract the verbatim source for any node via its span. The parser does not duplicate raw source text into the AST nodes themselves. Rationale: spans are a small extension of the per-node locations the spec already requires; raw text duplication would bloat the AST significantly for large projects; tools needing original text (`writ show`, error messages, IDE hover) extract on demand. Re-emitting from the AST is rejected because it would lose comments and developer-chosen formatting — fidelity matters for a "show what's there" tool.
- **Reserved words** — Keywords are contextual: reserved only in positions where they are grammatically meaningful. Statement keywords (`log`, `measure`, `session`, `csrf`, `limit`, `approve`, `resolve`, `commit`, `emit`, `format`, `redirect`, `layout`) are special only at statement-leading position inside a block. Block headers (`system`, `group`, `errors`, `include`, `<METHOD>`) are special only at column 0. Inline keywords (`with`, `using`, `OR`, `AND`, `NOT`, `body`, `query`, `none`, `default`) are special only in the contexts the grammar expects them. As segments of identifiers (e.g., `db.session.refresh`, `auth.with.something`), keyword words are unreserved and parse normally. Rationale: a small DSL with ~25 keywords would otherwise blacklist common-English words developers reasonably want in names; contextual keywords are trivial in recursive descent (one-token lookahead at each grammar point); matches the precedent of Go, SQL, and Kotlin.
- **Empty blocks** — A block header followed by no statements (ignoring blank lines and comment-only lines) is a parse error, with a message naming the block and its location. This applies to `system`, `group`, `errors`, and handler blocks alike. Rationale: an empty block is almost always an incomplete edit. The legitimate "intentional no-op" case is already served by explicit `none` statements (e.g., `approve none`), which makes intent visible. Catching empties at parse time is cheaper than later runtime complaints. Parser cost: a single "must have at least one statement" check per block.
- **`include` placement** — `include` statements may appear at any column-0 position in the root file. Includes are inlined in document order during flattening (an `include` between two handlers places the included file's contents between them in the resolved AST). Includes may themselves contain further `include` statements; the cycle-detection rule (already covered under *Include Resolution*) prevents infinite recursion. The single-system-block rule (`system` only in the root file) is the only ordering constraint. Rationale: flattening produces the same logical AST regardless of placement, and mid-file includes are a useful organizational tool (e.g., extracting a topic-grouped set of routes into its own file). Style preferences (group all includes together) belong to `writ fmt`, not the parser.
- **AST stability guarantee** — Pre-1.0, the AST is an *internal contract*, not a stable public API. The AST package is exported (the runtime, code generator, and tooling import it from a sibling package), but documented as unstable across minor versions. Internal Writ components are the only intended consumers; third-party users (LSP servers, custom linters, alternate code generators) may use it but accept that AST node shapes can change without notice. At Writ 1.0, the AST contract is reassessed and any parts with a clear external consumer story are promoted to stability guarantees. Rationale: the parser is being designed from a 1419-line README, not from production use; locking in AST stability before downstream specs land would commit to decisions made in the dark. Matches the standard Go ecosystem pre-1.0 pattern. The parser does not enforce this — the statement lives in package docs and the README.
