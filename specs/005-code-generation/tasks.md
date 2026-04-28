# 005 — Code Generation Tasks

Tasks derived from the [plan](plan.md). Complete in order.

## 1. Wire types in the runtime package

- [ ] Create `writ/wire.go` with `WireResolvers`, `WireFormatters`, `WireErrorFormatters`, `WireErrorTypes` as `map[string]any`-shaped types.
- [ ] Add doc comments per the data model.
- [ ] Create `writ/wire_test.go` confirming the types are addressable in `var _ = writ.WireResolvers{...}` form and have no runtime side effects (no init, no methods).
- [ ] Verification suite passes for the `writ` package.

**Done when:** `var _ = writ.WireResolvers{"db.users": SomeFunc}` compiles in a downstream test package; tests pass; `gofumpt`, `go vet`, `staticcheck`, `errcheck`, `golangci-lint`, `gosec`, `govulncheck`, `go test`, and coverage all clean.

## 2. Identifier generation

- [ ] Create `codegen/doc.go` with the package doc comment.
- [ ] Create `codegen/identifier.go` exporting `MakeIdentifier(method string, pattern *ast.RoutePattern) string` and the fixed acronym list (`ID`, `URL`, `API`, `HTTP`, `JSON`, `XML`, `UUID`, `IP`, `TCP`, `UDP`).
- [ ] Cover the resolved naming convention's examples: `GET /` → `GetRoot`; `GET /todos` → `GetTodos`; `GET /todos/:id` → `GetTodosByID`; `DELETE /todos/:id` → `DeleteTodosByID`; `GET /admin/todos/:id/comments/:cid` → `GetAdminTodosByIDCommentsByCID`.
- [ ] Cover the acronym list (`:url` → `ByURL`, `:api_key` → `ByAPIKey` since `API` is in the list).
- [ ] Cover `:tenant_id` → `ByTenantID` (mixed snake-case + acronym).
- [ ] Cover hyphenated method names per the parser regex (`X-CUSTOM` → `XCustom`, normalized).
- [ ] Create `codegen/identifier_test.go` table-driven over every example and edge case above.

**Done when:** `MakeIdentifier` is a pure function with deterministic output for every input; tests pass; verification suite clean for `codegen`.

## 3. Diagnostic kinds

- [ ] Create `codegen/diagnostic.go` with:
  - `Diagnostic struct{ Kind DiagnosticKind; Span ast.Span; Message string }`.
  - `DiagnosticKind` enum: `KindWireMissingEntry`, `KindWireStaleIdentifier`, `KindWireDuplicateKey`, `KindIdentifierCollision`.
  - `Diagnostic.Error()` formatting `file:line:col: kind: message`.
- [ ] Tests in `codegen/diagnostic_test.go` covering format and ordering when sorted.

**Done when:** Diagnostics format consistently with parser/pipeline; verification suite clean.

## 4. Wire discovery

- [ ] Add `golang.org/x/tools/go/packages` to `go.mod`; run `go mod tidy`.
- [ ] Create `codegen/wire/discover.go` exporting `Discover(pkgPath string) (Map, []codegen.Diagnostic, error)`. (Or, to avoid a back-import, define a local `Diagnostic` mirror and convert — final shape decided by the task author; the library boundary must not pull `codegen` into `wire`.)
- [ ] Implement loading via `go/packages` with mode `NeedTypes | NeedSyntax | NeedTypesInfo | NeedFiles | NeedCompiledGoFiles`.
- [ ] Walk every file's top-level `*ast.GenDecl`; for each `var _ = X{...}` pattern, resolve `X` via `pkg.TypesInfo.Types` and accept it iff the qualified type matches one of the four `writ.Wire*` types (alias-tolerant).
- [ ] Extract entries: each `*ast.KeyValueExpr` with a `*ast.BasicLit` STRING key and an `*ast.Ident` or `*ast.SelectorExpr` value.
- [ ] For `WireErrorTypes` entries, additionally call `pkg.TypesInfo.TypeOf(value)` to record the qualified Go type name.
- [ ] Detect duplicate DSL keys per role; emit `KindWireDuplicateKey`.
- [ ] Forward `pkg.Errors` (type-checker errors) re-framed as `KindWireStaleIdentifier` when they identify a wire entry's right-hand side.
- [ ] Build `Map.Resolvers`, `Map.Formatters`, `Map.ErrorFormatters`, `Map.ErrorTypes`.
- [ ] Tests in `codegen/wire/discover_test.go` using synthetic packages on disk (`t.TempDir()` + `os.WriteFile`) covering: every wire-type role, dotted DSL keys, qualified RHS identifiers (`pkg.Func`), aliased imports of `writ`, missing identifiers, duplicate keys.

**Done when:** Discovery returns a deterministic `Map` for every fixture; verification suite clean for `codegen/wire`.

## 5. Plan construction

- [ ] Create `codegen/plan.go` with `Plan`, `HandlerPlan`, `ParamSegment`, `ResolveCallPlan`, `ResolveArg`, `ResolveArgKind`, `FormatPlan`, `FormatKind`, `ErrorArmPlan` types per `data-model.md`.
- [ ] Implement `BuildPlan(resolved *pipeline.Resolved, wires wire.Map, sourceFiles []string, packageName string) (*Plan, []Diagnostic)`.
- [ ] For each `*pipeline.Handler`, compute `Identifier`, `RequestStructName`, `RegisterFuncName`.
- [ ] Walk `pipeline.ResolveStage`s in order; build `ResolveCallPlan`s; classify each arg via type assertion on `ast.Expr` (`*ast.RouteParamRef`, `*ast.FieldRef`, `*ast.NamedArg`); look up the Go function identifier in `wires.Resolvers`; emit `KindWireMissingEntry` if absent.
- [ ] Build `FormatPlan` from the terminal `*pipeline.FormatStage` or `*pipeline.RedirectStage`; look up the formatter identifier in `wires.Formatters`; emit `KindWireMissingEntry` if absent.
- [ ] Walk `Handler.ErrorMap` to build `ErrorArmPlan`s in order; for non-default entries, look up Go type in `wires.ErrorTypes` and Go formatter in `wires.ErrorFormatters`; emit `KindWireMissingEntry` for each absent.
- [ ] Detect `Identifier` collisions across the package; emit `KindIdentifierCollision` with both source spans.
- [ ] Sort `Plan.Handlers` by `(Method, PatternString)`; sort `Plan.SourceFiles` lexically.
- [ ] Tests in `codegen/plan_test.go` covering: every wire-miss kind, route-pattern shapes (root, single segment, multi-param), error map ordering preservation, identifier-collision detection, deterministic field order across two builds.

**Done when:** Two `BuildPlan` calls with the same inputs produce structurally identical `Plan`s (verified via `reflect.DeepEqual`); diagnostics cover every miss case; verification suite clean.

## 6. Emission

- [ ] Create `codegen/emit.go` exporting `Emit(plan *Plan, version string) ([]byte, error)`.
- [ ] Use `text/template` (or hand-written `bytes.Buffer` writes — author's choice) to produce the source.
- [ ] Emit the two-line header: `// Code generated by writ generate v{version}. DO NOT EDIT.` and `// Source: <comma-separated lexically-sorted .writ files>`.
- [ ] Emit `package <name>` and the minimal import set computed from emitted code (`context`, `errors`, `net/http`, the framework `writ` package). Skip imports unused by the package's actual handlers.
- [ ] For each `HandlerPlan` in sort order:
  - Emit the `<Identifier>Request` struct with one `string` field per `ParamSegment`.
  - Emit `Register{Identifier}(w *writ.Writ, ...)` whose typed parameters match the resolves and formatters declared.
  - Inside `Register{Identifier}`: emit closure adapters for each resolver and the formatter; each closure constructs the request struct from `Params`, calls the consumer-supplied typed function, stores results into the runtime; call `w.Resolver(...)`, `w.Formatter(...)`, `w.ErrorFormatter(...)`, `writ.ErrorType[T](w, ...)` as appropriate.
  - For handlers with non-empty error maps, emit an `errors.As(err, &t<i>)` chain with one arm per `ErrorArmPlan` in order; the trailing `default` (if present) becomes a fallthrough that does not call `errors.As`.
- [ ] Pipe final bytes through `go/format.Source` for canonical formatting.
- [ ] Verify the output has no `import "reflect"` directive and no aliased `reflect` import (regex check inside the test).
- [ ] Golden-file tests in `codegen/emit_test.go` and fixtures in `codegen/testdata/` covering: zero-handler package (no file emitted), one handler with one resolve, multi-resolve handler with all arg kinds, handler with `redirect`, handler with non-empty `errors` block, handler with `errors` `default` only, handler with both concrete and `default` arms.
- [ ] Determinism test: `Emit(plan, "0.0.0")` twice produces byte-identical output.

**Done when:** Goldens match exactly; determinism test passes; no-`reflect` test passes; verification suite clean for `codegen`.

## 7. Check mode

- [ ] Create `codegen/check.go` exporting `Check(generated, onDisk []byte) (diff string, ok bool)`.
- [ ] Use a unified-diff library or hand-roll a minimal LCS-based diff (`golang.org/x/tools/internal` is not importable; use `github.com/pmezard/go-difflib` if a dependency is preferred, or write a minimal diff producer — author's choice noted in commit message).
- [ ] Tests covering: identical inputs return `ok=true, diff=""`; differing inputs return `ok=false` with a non-empty diff.

**Done when:** Verification suite clean for `codegen`.

## 8. Scaffolding

- [ ] Create `codegen/scaffold/scaffold.go` exporting `Generate(prog *ast.Program, packageName string) ([]byte, error)`.
- [ ] Walk the AST to enumerate every resolver, formatter, error formatter, and error-type identifier referenced by handlers.
- [ ] Emit Go source containing:
  - `package <name>` (no `// Code generated ... DO NOT EDIT.` header — scaffolded files are consumer-owned).
  - One stub function per resolver, formatter, error formatter referenced; each stub has a body containing `// TODO: implement` and a return statement that satisfies the runtime function type signature.
  - One stub Go type per error-type DSL name (`type X struct{}` with a `func (X) StatusCode() int { return 500 }` method).
  - A populated wire block (`var _ = writ.WireResolvers{...}`, etc.) mapping every DSL name to the stub identifier.
- [ ] Pipe through `go/format.Source`.
- [ ] Golden-file tests in `codegen/scaffold/scaffold_test.go` and fixtures in `codegen/scaffold/testdata/` covering: simple handler, multi-handler file, handler with `errors` block, handler with `redirect`.
- [ ] Determinism test: same input produces byte-identical output.

**Done when:** Generated scaffolds compile in a fresh `t.TempDir()` directory paired with the `.writ` source and a subsequent `BuildPlan` + `Emit` produces a `writ_gen.go` that also compiles; verification suite clean for `codegen/scaffold`.

## 9. CLI binary

- [ ] Create `cmd/writ/main.go` with subcommand dispatch on `os.Args[1]`: `version`, `generate`, `generate scaffold`.
- [ ] Create `cmd/writ/version.go` with `const Version = "0.0.0"`.
- [ ] Create `cmd/writ/generate.go` implementing:
  - `generate` (no positional): walks current directory, calls `parser.Parse` for each `.writ`, calls `pipeline.Elaborate`, calls `wire.Discover`, calls `codegen.BuildPlan`, calls `codegen.Emit`, writes `writ_gen.go` via temp file + `os.Rename`.
  - `--dry-run`: same up through `Emit`; prints `would write <path>` on success; never writes.
  - `--check`: same up through `Emit`; reads on-disk `writ_gen.go`; calls `codegen.Check`; exits 0 on match, 1 with unified diff on stderr otherwise; never writes.
  - `generate scaffold <writ-file>`: reads the file, calls `parser.Parse`, derives package name, calls `scaffold.Generate`, writes `<stem>.go` if absent or with `--force`; exits 1 with a clear message on existing file without `--force`.
- [ ] Diagnostic streaming: any non-empty diagnostic set from any pass → print all to stderr in `file:line:col: kind: message` format, exit 1, write nothing.
- [ ] End-to-end tests in `cmd/writ/generate_test.go` using `t.TempDir()`:
  - Happy path: `.writ` + wire block + handlers → `writ generate` produces a `writ_gen.go` that `go build` accepts.
  - `--check` against in-sync file exits 0.
  - `--check` against out-of-sync file exits 1 with diff to stderr.
  - `--dry-run` prints intent and writes nothing.
  - Scaffold of a fresh `.writ` produces `<stem>.go`; second invocation without `--force` errors; with `--force` overwrites.
  - Parse error path: invalid `.writ` produces parser diagnostic on stderr, exit 1, no file written.
  - Missing wire entry path: valid `.writ` referencing an unwired resolver produces `KindWireMissingEntry` diagnostic, exit 1.
  - Stale wire identifier path: wire entry referencing an undefined Go function produces `KindWireStaleIdentifier`, exit 1.
- [ ] Verification suite passes for `cmd/writ`.

**Done when:** All E2E tests pass; binary built via `go build ./cmd/writ` produces a working CLI.

## 10. `go generate` integration

- [ ] Add a sample fixture under `codegen/testdata/gogenerate/` containing a Go file with `//go:generate writ generate` and a `.writ` source.
- [ ] Add an integration test that builds the `cmd/writ` binary, places it on `PATH` via `t.Setenv`, runs `go generate ./...` in the fixture directory, and asserts `writ_gen.go` is created.
- [ ] Verify the test passes from a clean state (no pre-existing `writ_gen.go`) and from a stale state (existing `writ_gen.go` is overwritten).

**Done when:** `go generate` invocation produces the same byte content as direct `writ generate` invocation; verification suite clean.

## 11. Acceptance suite

- [ ] Create `codegen/acceptance_test.go` walking every numbered acceptance criterion in `spec.md` and asserting each in turn. Where a criterion is structural (e.g., "no `import \"reflect\"`"), assert against generated output. Where a criterion is behavioral (e.g., "wrapped errors match the same registered type"), build a tiny end-to-end fixture and run a request through the resulting handler.
- [ ] Cover the runtime-compatibility criterion by loading the same `.writ` program twice — once via the runtime path (string-keyed `Resolver` registration), once via generated `Register{Identifier}` calls — and asserting identical responses to the same request.

**Done when:** Every acceptance-criteria checkbox in `spec.md` has a corresponding test invocation in this file; full verification suite clean across all affected packages.

## 12. README + docs

- [ ] Update `README.md`:
  - Add `writ generate` to the command list.
  - Add a "Code Generation" section describing wire blocks, scaffolding, `--check`, and the runtime-fallback compatibility guarantee.
  - Update Startup Validation to note that codegen-emitted registrations satisfy the existing validation kinds (no skip).
- [ ] Update the project structure section in `AGENTS.md` to list `cmd/writ/` and `codegen/`.

**Done when:** README and AGENTS.md reflect the new surface; markdownlint passes.

## 13. Status finalization

- [ ] Run the full per-package verification suite on `writ`, `codegen`, `codegen/wire`, `codegen/scaffold`, and `cmd/writ`.
- [ ] Coverage stays at or above the floor (`≥ 80%` general, `≥ 90%` for pure-logic `codegen`).
- [ ] `go mod tidy && git diff --exit-code go.mod go.sum` is clean.
- [ ] `go generate ./... && git diff --exit-code` is clean.
- [ ] Update `spec.md` status to `done` (only after every acceptance criterion has a passing test and the user approves).

**Done when:** The user approves the transition. Per the boundaries note in `AGENTS.md`, do not advance status without explicit user approval.
