# 005 — Code Generation

**Status:** planned
**Dependencies:** 001-dsl-parser, 002-pipeline-elaboration, 003-runtime-skeleton, 004-errors-block

The code generator turns parsed `.writ` programs into typed Go glue so consumer code interacts with handlers, resolvers, formatters, and error types through compile-time-checked identifiers instead of runtime string lookups and `any` casts. It is the long-term answer to the runtime's deferred typing decisions and the prerequisite for replacing reflection-using paths (the spec-004 `errors.As` matcher; the spec-003 `Params` and `Results` `any` accessors) with code that does not call into the standard library's reflection machinery at request time.

This spec covers the **mechanism** plus three concrete generated artifacts (typed route registration, typed parameter binding, and typed error-type dispatch) and a **scaffolding** mode that stamps out the consumer-side stubs the generator needs to consume on each subsequent run. Field accessors for `:model.attribute` references and SQL result scanning are out of scope because they depend on the future data-layer feature; this spec is sized to the maximal slice that does not depend on any spec that has not yet been written.

## In Scope

The generator runs as a CLI verb (`writ generate`) and as a `go generate` directive. It reads every `.writ` file in a directory tree (and the include graph each one references), parses them through spec 001, elaborates them through spec 002, and emits Go source files. The emitted files are checked in alongside the `.writ` files (not produced at build time inside a `// +build ignore` shim) so a consumer who clones the repository can `go build` without first running the generator.

The generator emits:

- **Typed route registration.** For every handler, a generated symbol that wires the route into a `*Writ` instance using the existing spec-003 registration surface. The consumer's hand-written code passes typed function values (not closures) to the generated symbol; the generator emits the closure adapters that bridge those typed functions to the runtime's `Params`/`Results`/`ErrorData` accessors. The generator validates at compile time that the function's signature matches what the handler's `resolve` step requires.
- **Typed parameter binding.** For every `:name` segment in a handler's route pattern, a generated field on a per-handler request struct. Resolvers receive the struct instead of the runtime `Params` accessor; reading `:id` becomes a struct field access, not a string-keyed map lookup.
- **Typed error-type dispatch.** For every handler with a non-empty effective error map, a generated typed switch that replaces the runtime `errors.As`-based matcher loop. Each `errors` block entry becomes a typed `case` branch; the generator picks up the developer's existing error-type declarations (still registered via the spec-004 `ErrorType[T]` mechanism) and emits the switch from them.

The generator preserves the runtime-only path. A consumer who chooses not to run `writ generate` continues to use the spec-003/004 string-keyed registration and `errors.As` matcher unchanged. Generated code is additive — it is the canonical path consumers should adopt, but the runtime remains fully functional without it.

### Discovery: how the generator knows DSL → Go mappings

The consumer expresses the mapping from DSL identifiers to Go identifiers in **wire declarations** — small typed Go expressions whose only purpose is to be parseable by the generator and type-checked by the Go compiler. The wire types live in the `writ` package and are shaped like `map[string]any`; the consumer assigns instances to `var _` (blank identifier) so they have no runtime effect.

```text
WireResolvers       — map of DSL resolver name → Go function value
WireFormatters      — map of DSL formatter name → Go function value
WireErrorFormatters — map of DSL error-formatter name → Go function value
WireErrorTypes      — map of DSL error-type name → zero-value of the Go type
```

The generator parses these declarations to learn which Go identifier implements each DSL name. Because the right-hand side of every entry is a typed Go expression, the Go compiler enforces that every named identifier exists; renaming a function on the consumer side without updating the wire entry is a compile-time error, not a silent drift.

Consumers do not write wire declarations from scratch. The scaffolding mode (described next) stamps them out from the `.writ` source.

## Out of Scope

Each item below has its own future feature spec or is deferred until a prerequisite spec exists.

- **Field accessors for `:model.attribute` references.** Requires the data layer feature to define what a "model" is. Generated field accessors will layer on top of this spec without changing its surface.
- **SQL result scanning.** Requires the data layer feature. The generator's parameter binding and route registration deliver value independently; SQL-side typed scanning is a future addition.
- **Typed body parsing (JSON, multipart form, query).** Requires the typed-input-structs feature.
- **Layout and template formatter generation.** Requires the HTML rendering feature.
- **Migration glue, CLI scaffolding, or test-DSL generation.** Each has its own spec area.
- **AST changes to `.writ` files.** The generator consumes whatever spec 001 parses today. Any new DSL syntax to support codegen-specific declarations is itself a spec change in 001 and is not bundled here.
- **Removal of the runtime fallback.** Even after codegen is shipped, the spec-003/004 string-keyed registration and `errors.As` matcher remain available. A future feature may deprecate one path or the other; this spec leaves both in place.
- **Generator-side optimizations beyond correctness.** The generator's output is correct, readable, and stable; performance tuning of the generated code (inlining hints, escape-analysis-friendly shapes, etc.) is a future enhancement.

## Generator Invocation

The generator exposes two CLI verbs that serve different purposes in the consumer's workflow:

- **`writ generate`** (continuous, machine-owned). Reads `.writ` files and the consumer's wire declarations, writes `writ_gen.go` next to the `.writ` files. Re-runs every time anything changes; the file is overwritten on each run. A `--dry-run` flag prints what would be written without modifying the filesystem.
- **`writ generate scaffold <writ-file>`** (one-time, consumer-owned). Reads a single `.writ` file and stamps out a `<stem>.go` file next to it containing typed function stubs (with `// TODO: implement` markers in their bodies), domain-type stubs, and a populated wire block. The consumer then edits the stubs to add real implementation. Subsequent runs of `writ generate` consume the consumer-edited file.

The scaffolding command refuses to overwrite an existing `<stem>.go` by default; a `--force` flag enables overwrite. A future enhancement (not part of this spec) is `writ generate scaffold --add` for appending only the new identifiers when a `.writ` file gains handlers after the initial scaffold, and `writ generate scaffold --check` for printing a diff without modifying anything.

Both verbs are also invocable via `go generate` directives. A consumer typically adds `//go:generate writ generate` to a Go source file in each package that contains `.writ` files; running `go generate ./...` invokes the continuous generator for every package that opts in. Scaffolding is run by hand at file-creation time and is not normally tied to `go generate`.

## Generated File Layout

The generator emits exactly one Go file per package, named `writ_gen.go`, written next to the `.writ` files in that package. The file contains the union of every handler the generator produces from the package's `.writ` sources.

The file begins with the canonical `// Code generated by writ generate. DO NOT EDIT.` header so the Go toolchain and code-review tooling recognize it as machine-owned: `gofmt` skips it, code review UIs collapse it, `go doc` excludes it. The file is checked in so consumers who clone the repository can `go build` without first running the generator.

Re-running `writ generate` overwrites the file. Local edits are not preserved. Removing a `.writ` from a package and re-running the generator rewrites `writ_gen.go` to omit the deleted handler's symbols; no stale per-file artifact is left on disk.

The choice of one file per package (rather than one file per `.writ`) is fixed by the generator and is not configurable per project. This matches the convention of `sqlc`, `ent`, `protoc-gen-go`, and `mockgen` and avoids the stale-orphan-file hazard that per-file generation introduces when a `.writ` source is deleted.

## Idempotence and Determinism

Running `writ generate` twice on the same input produces byte-identical output. The generator does not embed timestamps, version stamps, or random identifiers in its output. Names of generated identifiers are deterministic functions of the originating `.writ` source: handler method, route pattern, `:name` segments, and resolver/formatter/error-type identifiers.

When the generator's output would change a file already on disk, it overwrites that file. The generator does not leave the consumer to merge changes. A pre-commit hook or CI check that runs `writ generate` and fails when generated files drift from their inputs is a future enhancement, not part of this spec.

## Drift Detection

The generator detects two classes of drift between the consumer's Go code and the generated glue:

- **Signature drift.** A consumer-supplied resolver function whose signature does not match the handler's declared shape produces a Go compile error referencing the generated symbol's expected signature. The generator does not silently accept misshapen functions.
- **Missing identifier drift.** A `.writ` file that references a resolver, formatter, or error-type name with no corresponding consumer-side implementation produces a Go compile error at the generated symbol's call site, referencing the missing identifier. This is a stricter check than the spec-003 startup-time `KindUnregisteredResolver` entry: the latter fires at runtime; this catches it at `go build` time.

Drift is detected by the Go compiler operating on generated code, not by a custom static analyzer the generator ships. The generator's only responsibility is producing code that the Go compiler can type-check; the type checker does the work.

## Integration with the Runtime

The runtime introduced in spec 003 does not change shape. The `Writ` struct, the `New` constructor, the registration methods, and the `Load` and `Handler` lifecycle remain exactly as spec 003 describes. Generated code calls into those existing entry points; it does not require new runtime APIs.

The generator may emit additional helpers that wrap or compose the runtime's existing surface, but it does not introduce a parallel runtime. There is one runtime; codegen is a layer on top of it.

The spec-004 `ErrorType[T]` registration remains in place. Generated typed-switch dispatch reuses the registered types; the generator does not invent its own type registry.

## DSL Changes

This spec introduces no changes to the `.writ` grammar parsed by spec 001. The generator's input is the AST that spec 001 already produces.

If a future feature needs new DSL syntax to express codegen-specific declarations (e.g., explicit parameter type annotations such as `:id int`), that change is a spec 001 update. This spec does not bundle grammar changes.

## Acceptance Criteria

### Generator binary

- [ ] `writ generate` is a CLI verb on the `writ` binary.
- [ ] Running `writ generate` in a directory that contains a `.writ` file produces a `writ_gen.go` file next to it.
- [ ] Running `writ generate --dry-run` prints the names of files that would be written but does not modify the filesystem.
- [ ] Running `writ generate --check` computes the would-be output in memory, compares it byte-for-byte to the on-disk `writ_gen.go`, and exits zero when identical.
- [ ] Running `writ generate --check` against an out-of-date `writ_gen.go` exits non-zero and writes a unified diff to stderr.
- [ ] `writ generate --check` does not modify the filesystem regardless of outcome.
- [ ] `writ generate` returns a non-zero exit code when the input `.writ` files fail to parse or elaborate.
- [ ] A `//go:generate writ generate` directive in a Go source file produces the same output as running the CLI verb directly in that package's directory.

### Scaffolding

- [ ] `writ generate scaffold <writ-file>` is a CLI verb on the `writ` binary.
- [ ] Running scaffolding against a `.writ` file with no existing `<stem>.go` next to it produces that file with typed function stubs, domain-type stubs, and a populated wire block.
- [ ] Each generated function stub has a body containing a `// TODO` marker and a return statement that satisfies the function's signature (so the package compiles immediately after scaffolding).
- [ ] The generated wire block references every resolver, formatter, error formatter, and error type the `.writ` source declares; running `writ generate` immediately after `writ generate scaffold` succeeds without further consumer edits.
- [ ] Running scaffolding when `<stem>.go` already exists returns a non-zero exit code with a message that names the file and instructs the consumer to use `--force` to overwrite.
- [ ] Running scaffolding with `--force` overwrites the existing `<stem>.go`.
- [ ] Scaffolded files are NOT marked `// Code generated ... DO NOT EDIT.` — they are consumer-owned from the moment of creation; the framework never regenerates them.

### Wire declarations

- [ ] The `writ` package exports `WireResolvers`, `WireFormatters`, `WireErrorFormatters`, and `WireErrorTypes` as types whose values can be expressed in Go source as `map[string]any`-shaped composite literals.
- [ ] Wire declarations have no runtime effect — the generator parses them at codegen time only; the runtime never reads them.
- [ ] A wire entry whose right-hand-side identifier does not exist in the package fails to compile with the standard Go "undefined: X" error.
- [ ] The continuous `writ generate` reads every wire declaration in the package and uses the (DSL name → Go identifier) mapping to emit closure adapters, registration calls, and typed switch arms.
- [ ] Renaming a Go function on the consumer side without updating the corresponding wire entry fails to compile at the wire entry's source location.

### Generated output basics

- [ ] Generated files live next to their originating `.writ` files in the same Go package.
- [ ] Generated files compile under `go build` with no consumer-side modifications, given resolver/formatter/error-type implementations whose Go signatures match the spec.
- [ ] Generated files contain no `import "reflect"` directive.
- [ ] Generated files contain no calls into the `reflect` package via any aliased import.
- [ ] Two consecutive runs of `writ generate` against the same input produce byte-identical files within a single Go toolchain version. Output may shift cosmetically across Go toolchain upgrades (struct tag alignment, line wrapping, build-constraint canonicalization); semantic content — identifiers, types, registration calls, switch arms — does not change.
- [ ] Every generated file begins with two header comment lines: `// Code generated by writ generate v{X.Y.Z}. DO NOT EDIT.` and `// Source: <comma-separated lexically-sorted list of .writ files in the package>`. The version is the `writ` binary's own version (matches `writ version` output).

### Typed route registration

- [ ] For every handler in the input, a generated registration symbol exists that the consumer can use to wire the handler to a `*Writ` instance.
- [ ] The generated registration symbol's signature requires a function value whose shape matches the handler's `resolve` and `format` step signatures, including each step's parameter and return types.
- [ ] A consumer that supplies a function with a mismatched signature produces a Go compile error at the registration site.
- [ ] A consumer that omits a required registration symbol produces a Go compile error at startup-glue invocation.

### Typed parameter binding

- [ ] Every `:name` segment in a handler's route pattern produces a typed field on a generated per-handler request type.
- [ ] Resolvers receive the generated request type instead of the runtime `Params` accessor when the consumer uses the generated registration glue.
- [ ] The generated request type's field for a `:name` segment defaults to `string` when no other type information is available; future features that introduce parameter type annotations layer on top.

### Typed error-type dispatch

- [ ] For every handler whose effective error map is non-empty, the generator emits a typed dispatch construct (e.g., a `switch err := err.(type)` block) that replaces the spec-004 runtime `errors.As` loop for that handler.
- [ ] The generated dispatch invokes the same registered error formatter the spec-004 runtime would invoke for each entry in the handler's effective error map.
- [ ] The generated dispatch preserves the spec-004 ordering rules: concrete-type entries are checked before the `default` entry, and the first match wins.
- [ ] A handler with no `errors` block produces no generated dispatch construct; the runtime fallback continues to apply.
- [ ] Wrapped errors (via `fmt.Errorf` with `%w`) match the same registered type they would match under the spec-004 runtime path.

### Determinism and drift

- [ ] Running `writ generate` against the same input twice (under the same Go toolchain) produces byte-identical output, including identifier names, comment ordering, and file ordering.
- [ ] A consumer-side rename of a resolver Go function produces a Go compile error against the generated registration symbol that names the missing function.
- [ ] A `.writ`-side change to a route pattern (e.g., adding a new `:name` segment) regenerates the request type with the new field; consumers that referenced the field name from their own code see a Go compile error if the segment is removed in a later edit.

### Runtime compatibility

- [ ] A consumer that does not run `writ generate` continues to load and serve `.writ` programs using the spec-003 runtime registration and the spec-004 `errors.As` matcher unchanged.
- [ ] A consumer that runs `writ generate` and adopts the generated registration glue produces identical request behavior — same status codes, same response bodies, same Allow header on 405 — as the same program loaded through the runtime path.
- [ ] Generated code compiles and runs against the existing public surface introduced in specs 003 and 004; no new exported runtime methods or types are required.

## Open Questions

*All open questions resolved.*

## Resolved Questions

- **Runtime fallback removal path** — Defer entirely to a future spec. Spec 005 ships codegen as purely additive: the runtime path is unchanged and unmarked. No build tag, no deprecation flag, no opt-in mode is introduced. Rationale: a future deprecation hook should be designed by the spec that does the deprecating, with full knowledge of how codegen actually got adopted (wholesale vs. selective) — today we don't have that evidence. Adding a hook now would also violate spec 005's explicit commitment that "the runtime introduced in spec 003 does not change shape." When a future spec wants to deprecate the runtime path, it can add whatever hook it needs at that time; the absence of a hook today does not constrain that future design — hook introduction is purely additive whenever it lands.
- **Versioning of generated code** — Include the `writ` CLI version in the file header. The generator emits two header comment lines at the top of `writ_gen.go`: `// Code generated by writ generate v{X.Y.Z}. DO NOT EDIT.` (using the `writ` binary's own version, matching `writ version` output) and `// Source: <comma-separated lexically-sorted list of .writ files in the package>`. Both lines are deterministic within a `(writ-CLI-version, Go-toolchain-version)` pair. Rationale: every Go codegen tool the consumer is likely to encounter (`protoc-gen-go`, `sqlc`, `mockgen`) embeds its version; omitting it would be the surprise. Debugging payoff (knowing what version produced a file when a regression is reported) outweighs the cost (CI churn on CLI upgrades, which forces the consumer to acknowledge the upgrade in their commit history rather than silently inherit it). The Q8 caveat about per-toolchain stability already accepts that consumers regen-and-commit on Go upgrades; CLI version bumps fold into the same workflow. The `--check` mode (Q10) compares the full file content including the version line, so CLI upgrades cause `--check` to fail until the consumer regenerates — that's the expected gate.
- **Bootstrap order (circular import risk)** — No circular import exists; no special precaution needed. Import graph is acyclic by construction: the consumer's `main` imports their `api` package; their `api` package (and the generated `writ_gen.go` within it) imports the framework's `writ` package; the framework never imports anything from the consumer. The generator binary itself depends on `parser`/`pipeline`/`ast` and `go/packages` for AST walking — it does NOT depend on the runtime `writ` package, so even the generator's own import graph is clean. Concrete commitment: the wire types (`WireResolvers`, `WireFormatters`, `WireErrorFormatters`, `WireErrorTypes`) live in package `writ` alongside the runtime types they wire, not in a separate `writ/wire` sub-package, so consumers need only one import (`import "github.com/stonean/writ/writ"`). A sub-package would technically work (no circular risk either way) but would force two imports for no benefit.
- **`writ generate` and CI hygiene** — Bundled in spec 005 as `writ generate --check`. The mode runs the generator in memory, compares the would-be `writ_gen.go` content byte-for-byte against the on-disk file, exits zero when identical and exits non-zero with a unified diff to stderr when different. It does not modify the filesystem. Mechanism is essentially `--dry-run` plus a comparison; no significant new code paths. Pairs with the per-toolchain determinism guarantee — without `--check`, that guarantee is theoretical; with `--check`, it's the operational gate consumers wire into CI as a one-line `writ generate --check` step. Per-toolchain stability caveat applies: `--check` will fail across Go version upgrades, which is expected (regenerate and commit when upgrading). Scaffolding has its own future `--check` mode (see Generator Invocation) that is informational rather than a workflow gate, since scaffolded files are consumer-owned.
- **Interaction with the `KindUnregisteredResolver`, `KindUnregisteredFormatter`, `KindUnregisteredErrorType`, and `KindUnregisteredErrorFormatter` validation passes** — Runtime checks always run, including for codegenned handlers. Codegen does not emit a "skip validation" signal and does not bypass the runtime registries. Rationale: spec 005 already commits to "the runtime introduced in spec 003 does not change shape" — adding a skip-flag (Option B) or a parallel typed dispatch path (Option C) would violate that. The cost of always-running the checks is microscopic — they execute once at `Load` time, not per request, and add nanoseconds to startup. The runtime check also serves as a defense-in-depth line: a future generator bug that emits registration code in unreachable form would still be caught by the runtime's startup pass. Mixing codegen and hand-rolled registration in the same program is supported uniformly under this rule; the runtime treats both kinds of handlers identically.
- **Generator output stability across Go toolchain versions** — Per-toolchain stability only. The promise is: running `writ generate` twice with the same Go toolchain produces byte-identical files. Across toolchain versions, output may shift cosmetically (struct tag alignment, line wrapping, build-constraint canonicalization) but semantic content (identifiers, types, registration calls, typed switch arms) does not change. The generator uses stdlib `go/format` for output rather than pinning a custom printer; this matches the practice of `sqlc`, `ent`, and `protoc-gen-go`. Rationale: pinning a printer is real maintenance overhead for a problem most consumers won't hit (toolchain upgrades are quarterly at most). When the team upgrades Go, they regenerate once and commit the diff — the same workflow Go projects already use for `gofmt` shifts. CI workflow consequence (out of scope but flagged): `writ generate --check && git diff --exit-code` against a Go version pinned via `go.mod` and `actions/setup-go`.
- **Generated identifier naming convention** — `{Method}{PascalCasePath}` with role suffixes. The HTTP method is TitleCased (`Get`, `Post`, `Put`, `Delete`, `Patch`). The path strips its leading slash, splits on `/`, and converts each segment: literal segments become `PascalCase`, parameter segments become `By{PascalCase}`, the root path becomes `Root`. Symbols then take role suffixes — registration function `Register{Name}`, request struct `{Name}Request`, internal helpers stay unexported. A small fixed acronym list (`ID`, `URL`, `API`, `HTTP`, `JSON`, `XML`, `UUID`, `IP`, `TCP`, `UDP`) is uppercased per Go style; anything not in the list is title-cased normally. Examples: `GET /todos` → `GetTodos`; `GET /todos/:id` → `GetTodosByID`; `DELETE /todos/:id` → `DeleteTodosByID`; `GET /admin/todos/:id/comments/:cid` → `GetAdminTodosByIDCommentsByCID`. Identifier collisions in the same package — two handlers whose method+path mechanically reduce to the same Go identifier — produce a generator error per Q6 (write nothing, exit non-zero, point at both `.writ` source spans), not a silent disambiguation suffix. Rationale: predictability and stability beat brevity. The consumer references these names in their wire block and `main.go`; an unpredictable rule would force a `writ_gen.go` round-trip every time. The method prefix is verbose for single-method APIs but pays for itself in REST-style APIs and keeps the rule uniform. Long names for deeply nested routes are accepted as a signal pointing at the DSL-side concern, not a generator concern. Source-location naming (e.g., `Todos1`, `Todos2`) was rejected because it breaks when handlers are reordered.
- **Generator behavior when input is invalid** — Atomic outcome: on any failure, write nothing to disk, exit non-zero (single exit code 1), and emit collected diagnostics with `file:line:col: kind: message` formatting. Four failure classes, each with defined behavior: (a) **parse errors** reuse spec 001's diagnostics verbatim; (b) **elaboration errors** reuse spec 002's diagnostics verbatim; (c) **wire-side mismatch** (a `.writ` references a DSL name with no wire entry) is a generator-emitted diagnostic naming the DSL name, the `.writ` source span, and the wire block where the entry should live; (d) **wire-side stale** (a wire entry references a Go identifier that doesn't exist) surfaces the Go compiler / `go/packages` "undefined" error reframed with the wire block's source location. Multiple errors in one run are collected and reported together rather than short-circuited (matches spec 001/002 behavior). `--dry-run` still emits diagnostics and still exits non-zero on failure — "dry" refers only to filesystem writes. Rationale: standard "compiler hygiene" — fail loud, fail safe, fail with locations. The atomic-write rule preserves yesterday's working `writ_gen.go` when today's `.writ` edit breaks the build, so consumers can fix forward without losing a working state.
- **Default type for `:name` segments** — `string`. Every `:name` segment becomes a `string` field on the per-handler request struct. The generator provides no escape hatch (no `WireParamTypes` block or similar) for declaring other types in this iteration. Rationale: path parameters arrive from `net/http` as strings; anything else is a coercion that can fail, and spec 005's job is to faithfully type what the runtime delivers, not invent new failure modes. Adding an out-of-band type annotation now would create a parallel grammar — when a future spec 001 update introduces `:id int` syntax, there would be two authoritative sources for parameter types (the DSL and the wire block) and a precedence rule to invent. Better to leave the typed-parameter surface entirely to the future spec 001 update so the DSL remains the single source of truth. Consumer-side coercion (`id, err := strconv.Atoi(req.ID)`) is one line and forces explicit handling of coercion failures, which is a feature in the meantime. Forward path: when spec 001 adds parameter type annotations, spec 005 reads them and emits typed fields with coercion inside the generated closure adapter; existing `string`-using consumers see a compile break the next time they regenerate, which is the correct opt-in signal.
- **Per-handler request struct vs. single shared shape** — Per-handler. Each handler gets its own request struct (`GetTodoRequest`, `UpdateTodoRequest`, etc.); even handlers whose `:name` segments overlap get separate types. Rationale: the per-handler struct mirrors Fastify's per-route schema model, which is the canonical anchor in that ecosystem for request validation, JSON deserialization, type coercion, response validation, and OpenAPI documentation generation. Per-handler also matches spec 003's existing contract that resolvers see only the parameters their handler's call site lists (`paramsForCall` enforces this at runtime today; per-handler structs make it a compile-time guarantee). The trade-off — N handlers in a package produces N generated structs — is not a real cost: the structs are flat, small, and live in one machine-owned file the consumer doesn't browse. Single-shared was rejected because it would let a resolver accidentally read parameters its handler never declared (defeating the spec's promise of typed scope) and would force every consumer of `req.ID` into cascading break-changes when a future spec 001 update adds parameter type annotations like `:id int`. Validation tags, body/query/header binding, and OpenAPI generation are explicitly out of scope for this spec but become natural additive extensions of the per-handler struct in future features; this resolution preserves that surface.
- **Generator discovery of DSL → Go mappings (resolvers, formatters, error formatters, error types)** — The consumer expresses the mapping in **wire declarations**: typed Go assignments of the form `var _ = writ.WireResolvers{"db.users": GetUserByID, ...}` (and parallel `WireFormatters`, `WireErrorFormatters`, `WireErrorTypes` blocks). The generator parses these declarations to learn the mapping; the Go compiler enforces that every right-hand-side identifier exists. To avoid asking the consumer to hand-write the wire block, a `writ generate scaffold <writ-file>` CLI verb stamps out a starter `<stem>.go` containing typed function stubs (with `// TODO` markers), domain-type stubs, and a populated wire block. Subsequent `writ generate` runs consume the consumer-edited file. Rationale: this collapses two questions (resolver discovery, error-type discovery) into one mechanism; the wire block is explicit, compile-checked, and scales linearly (one line per registered name); scaffolding eliminates the from-scratch boilerplate burden the wire block would otherwise impose. Trade-offs considered: a naming-convention discovery (no wire block) was rejected because the DSL's dotted form `db.todos.list` has no clean canonical Go form and forces consumers off idiomatic verb-first names; a generic-typed registration helper that infers signatures at the call site can express resolver inputs but not their typed outputs without per-input adapters the generator would still need to emit; preserving spec-003 closures and codegen-ing only typed accessors fails to deliver the spec's core promise (replacing reflection in the request path) and makes spec 005 not worth shipping standalone.
- **Generated file layout** — One file per Go package, named `writ_gen.go`, written next to the `.writ` files in the same package. The file is the union of every handler the generator produces from the package's sources, carries the canonical `// Code generated by writ generate. DO NOT EDIT.` header, and is checked in so consumers who clone can `go build` immediately. Re-running the generator overwrites the file; removing a `.writ` and regenerating rewrites the union with no stale orphans. Rationale: per-file generation requires the generator to track and clean up orphaned files when a `.writ` is removed (every per-file codegen tool ends up needing a manifest sweep step); per-package output sidesteps that entirely. Per-package also lets the generator share unexported helpers across handlers without inventing a third "common" file. Matches the convention of `sqlc`, `ent`, `protoc-gen-go`, and `mockgen`. Trade-off accepted: a single-handler edit produces a diff that touches the whole `writ_gen.go`, but the generator is deterministic so the diff is minimal in practice.
