# 003 — Runtime Skeleton Tasks

Tasks derived from the [plan](plan.md) and [data model](data-model.md). Complete in order. Each task has a clear definition of done; do not move on until that condition is met.

## 1. Package skeleton and constants

- [x] Create `writ/doc.go` with the package comment declaring `writ` as the runtime entry point and exported but unstable pre-1.0 (matching the AST and pipeline packages).
- [x] Create `writ/writ.go` with the `Writ` struct (unexported fields per data-model), the `New() *Writ` constructor, the `defaultPort` and `defaultWritEnv` constants, and the lifecycle state constants (`stateInit`, `stateLoading`, `stateLoaded`).
- [x] Confirm `go build ./writ/...` and `go vet ./writ/...` are clean on the empty package.

**Done when:** the package compiles and `go vet` is clean.

## 2. Public accessor types: `Params` and `Results`

- [x] Create `writ/params.go` with the `Params` struct, `String(name) string`, and `Has(name) bool`.
- [x] Create `writ/results.go` with the `Results` struct, `Get(name) any`, and `Has(name) bool`.
- [x] Add unit tests in `writ/params_test.go` and `writ/results_test.go` covering present-key, absent-key (zero value), and `Has` true/false cases.

**Done when:** unit tests pass and accessors return the documented zero values for missing keys.

## 3. Function signature types and registration

- [x] In `writ/register.go`, declare `ResolverFunc` and `FormatterFunc` per the data model.
- [x] Implement `Resolver(name string, fn ResolverFunc) error` and `Formatter(name string, fn FormatterFunc) error`. Both:
  - return an error if `name` is already registered;
  - panic with a clear message if called when state is not `stateInit`.
- [x] Add unit tests in `writ/register_test.go` covering: successful registration, duplicate-name error, post-`Load()` registration panic.

**Done when:** every Construction-and-Registration acceptance criterion has a passing test.

## 4. Startup error type

- [x] Create `writ/error.go` with `ErrorKind` constants, `ErrorKind.String()`, `Entry`, and `Error` per the data model.
- [x] Implement `Error.Error() string` formatting each entry as `file:line:col: message` joined by newlines.
- [x] Implement `Error.Unwrap() []error` returning each `Entry` wrapped as an individual error so `errors.As` works. (`errors.Is` against Entry values is not supported because Entry contains a slice field; `errors.As(*Error)` plus iterating `Entries[i].Kind` is the supported pattern.)
- [x] Add unit tests in `writ/error_test.go` covering: empty `Error`, single-entry formatting, multi-entry formatting in source order, `errors.As(*Error)`, and per-entry `Unwrap()`.

**Done when:** every formatting and unwrap path is exercised and `errors.As` against the aggregate works as documented.

## 5. Route compilation and matching

- [x] Create `writ/route.go` with `routingTable`, `compiledRoute`, `resolveStep`, and `formatStep` per the data model.
- [x] Implement `compileRoutes(resolved *pipeline.Resolved, resolvers map[string]ResolverFunc, formatters map[string]FormatterFunc) (*routingTable, []Entry)`. Walk every handler in `resolved.Handlers`:
  - For each `pipeline.Stage`, dispatch on `Kind()`: `StageResolve` → append a `resolveStep` with the registered fn (or emit `Entry{Kind: KindUnregisteredResolver}`); `StageFormat` → set `format` with the registered fn (or emit `Entry{Kind: KindUnregisteredFormatter}`); anything else → emit `Entry{Kind: KindUnsupportedStage}`.
  - Validate every `:name` argument against `paramNames`; emit `Entry{Kind: KindUndeclaredRouteParameter}` for misses. Non-`:name` argument shapes (NamedArg literals, FieldRef, BodyRef, QueryRef) are reported under the same kind with a clarifying message because the skeleton has no separate "unsupported argument shape" kind.
  - Append the compiled route to `byMethod[handler.Method]` and add the method to `methods` (deduped, sorted at the end).
- [x] Implement `(*routingTable).match(method, path string) (*compiledRoute, Params, []string)`:
  - Walk `byMethod[method]` in declaration order; for each route compare segment count; segment-by-segment compare literal/parameter; first match wins, return the route plus a populated `Params`.
  - On no match for the method, walk every method's routes computing the path-only match set; return `nil, Params{}, sorted methods` for a 405 signal.
  - On no match anywhere, return `nil, Params{}, nil` for a 404 signal.
- [x] Add unit tests in `writ/route_test.go` covering: literal-only path, parameter binding, segment-count mismatch, method mismatch with sorted `Allow`, trailing-slash strict (`/users` vs `/users/`), multi-method same-path Allow construction.

**Done when:** every Routing acceptance criterion has at least one passing test, and `match` returns deterministic output across repeated calls.

## 6. Validation pass and route ambiguity

- [x] Create `writ/validate.go` with `validate(resolved *pipeline.Resolved, resolvers, formatters) []Entry` orchestrating the per-handler walk: defensively check every handler ends in exactly one `format` step (per `system.md` *Pipeline shape* — the elaborator already enforces this for the in-scope subset, but re-check), then call `compileRoutes` from task 5 to surface unsupported-stage / unregistered-name / undeclared-parameter entries.
- [x] Add a route-ambiguity check that groups handlers by `(method, canonicalPath)` (canonical path replaces parameter segment names with `:` so `/users/:id` and `/users/:user_id` collide) and emits a `KindRouteAmbiguity` `Entry` per group with size > 1, with the first handler's span as `Span` and the rest as `Spans`.
- [x] Add unit tests in `writ/validate_test.go` with one test per `ErrorKind` (`KindUnregisteredResolver`, `KindUnregisteredFormatter`, `KindUnsupportedStage`, `KindUndeclaredRouteParameter`, `KindRouteAmbiguity`) asserting both `Kind` and `Message` shape.

**Done when:** every Loading-and-Validation acceptance criterion that maps to a runtime-validation pass has at least one passing test.

## 7. Lifecycle: `Load` orchestration

- [x] Create `writ/load.go` with `Load(path string) error` running the five passes in order: `parser.Parse` → `pipeline.Elaborate` → `validate` → ambiguity check → `compileRoutes`. Translate parser errors to `Entry{Kind: KindParseFailure}` and elaborator errors to `Entry{Kind: KindElaborationFailure}`. Short-circuit on parse or elaboration failure (do not run subsequent passes).
- [x] Implement the lifecycle state machine using `atomic.Uint32`: CAS `stateInit → stateLoading`; on CAS failure when the current state is `stateLoading`, panic with a message naming the concurrent-load violation; on CAS failure when the current state is `stateLoaded`, return an "already loaded" error. On compilation failure roll back to `stateInit`. On success install the `routingTable` via `atomic.Pointer` and CAS `stateLoading → stateLoaded`.
- [x] Add unit tests in `writ/load_test.go` covering: parse failure short-circuit (no validation entries), elaboration failure short-circuit, validation failure with non-nil aggregate `*Error`, double-load returns "already loaded" error, concurrent-load panic via two `sync.WaitGroup`-coordinated goroutines.

**Done when:** every Loading acceptance criterion has a passing test, and the concurrent-load test reliably reproduces the panic with the documented message.

## 8. Request dispatch

- [x] Create `writ/dispatch.go` with `(*Writ).ServeHTTP(http.ResponseWriter, *http.Request)`, `writeRecorder`, `paramsForCall(params Params, paramArgs []string) Params`, `buildResults(resultsMap map[string]any, with []string) Results`, and `write500(w http.ResponseWriter)`. (Dropped the `msg` argument from `write500` — the message would only be useful for future logging features and was unused at every call site, per the constitution's no-speculative-API rule.)
- [x] `ServeHTTP` flow: load state guard (defensive), `routingTable.match`, on miss/405 write the appropriate response with sorted `Allow`, on hit run resolves sequentially writing the resolver result into a per-request `map[string]any`, then call the formatter via the wrapped `writeRecorder`. On resolver error or pre-write formatter error, call `write500`.
- [x] `Handler() http.Handler`: panic when state is not `stateLoaded`; otherwise return `w` (which implements `http.Handler` via `ServeHTTP`).
- [x] Add unit tests in `writ/dispatch_test.go` covering: 200/JSON happy path with parameter binding (use `httptest.NewServer`), multiple sequential resolves with results passed to formatter, resolver-error → 500 + `Content-Type: text/plain; charset=utf-8`, formatter pre-write error → 500, formatter-status-override (e.g., 201) preserved, zero-resolve handler → empty `Results`, formatter receives only `with`-listed names, request isolation (two concurrent requests do not share state).

**Done when:** every Resolve, Format, and Lifecycle/HTTP-Boundary acceptance criterion has a passing test.

## 9. `Run` convenience and `gosec` annotation

- [x] Add `Run(path string) error` to `writ/writ.go`: call `Load(path)`, then `os.Getenv("PORT")` (default `defaultPort`), then `http.ListenAndServe(":"+port, w.Handler())`. Annotate the `ListenAndServe` call with `// #nosec G114 -- timeout policy deferred per spec 003 Q12` (gosec uses `#nosec` directives, not `//nolint:gosec`).
- [x] Add a unit test in `writ/run_test.go` that asserts `Run` returns the underlying load error on a bad path without ever binding a port.
- [x] Confirm `gosec -quiet ./writ/...` exits cleanly with the `#nosec` annotation in place.

**Done when:** the `Run` happy path is documented by a unit test that verifies its `Load` orchestration, the `gosec` suppression is annotated, and the security scan is clean.

## 10. Acceptance criterion test pass

Cover every checkbox under "Acceptance Criteria" in `spec.md`. Group tests by spec section. Use `parser.ParseString` to build `.writ` programs in-memory, or `testdata/*.writ` fixtures where larger sources help readability.

Coverage map: most checkboxes are covered by per-component test files; `acceptance_test.go` adds the gaps and end-to-end coverage that exercises Load + Handler through `httptest`.

- [x] **Construction and Registration** — covered in `register_test.go` (`TestResolverDuplicateNameReturnsError`, `TestResolverPanicsAfterLoad`, etc.) and `dispatch_test.go` (`TestHandlerPanicsBeforeLoad`).
- [x] **Loading and Validation** — covered in `load_test.go`, `validate_test.go`, and `error_test.go`. `acceptance_test.go` adds `TestAcceptanceMultipleUnregisteredNamesInSingleLoad` and `TestAcceptanceParseErrorsCarryFileLineColumn` for the "multiple in single pass" and "carries source location" criteria.
- [x] **Routing** — covered in `route_test.go` (synthetic table) and `dispatch_test.go` (end-to-end). `acceptance_test.go` adds `TestAcceptanceMultiMethodSamePathBothDispatch`, `TestAcceptanceTrailingSlashStrictEndToEnd`, and `TestAcceptanceExactSegmentMatching` for end-to-end coverage of the routing rules.
- [x] **Resolve** — covered in `dispatch_test.go` (`TestDispatchHappyPath`, `TestDispatchMultipleResolvesInOrder`, `TestDispatchResolverErrorWritesGeneric500`, `TestDispatchZeroResolveHandlerProducesEmptyResults`).
- [x] **Format** — covered in `dispatch_test.go` (`TestDispatchHappyPath`, `TestDispatchFormatterCustomStatusPreserved`, `TestDispatchFormatterDefaultStatusIs200`, `TestDispatchFormatterErrorBeforeWriteWrites500`, `TestDispatchFormatterReceivesOnlyWithListedNames`).
- [x] **Lifecycle and HTTP Boundary** — `acceptance_test.go` adds `TestAcceptanceHandlerComposesWithMiddleware`, `TestAcceptanceResolverPanicPropagates`, `TestAcceptanceFileDeletedAfterLoadStillServes`, `TestAcceptanceCallerHTTPServerTimeoutsApplyUnmodified`, and `TestAcceptanceConcurrentLoadPanicRecoverable`. The "`Run` reads `PORT`" criterion is covered by `run_test.go`'s decomposition assertion (Run propagates Load failures without binding) plus the constant `defaultPort` test surface.
- [ ] **Determinism and Isolation** — per-request isolation covered in `dispatch_test.go` (`TestDispatchRequestIsolation`); `Load` determinism covered in task 12.

**Done when:** every acceptance-criteria checkbox in `spec.md` has at least one corresponding passing test, and `go test ./writ/... ./pipeline/... ./parser/... ./ast/...` is green.

## 11. Smoke test fixture

- [x] Create `writ/testdata/smoke.writ` with two handlers exercising the in-scope envelope: one parameterized `GET /users/:id` with one resolve and one format; one `GET /users` with zero resolves and one format.
- [x] Add `writ/smoke_test.go` that loads the fixture via `Load`, serves via `httptest.NewServer`, and exercises: a 200 path with parameter binding, a 405 path, a 404 path, and a deliberate resolver error producing a 500.

**Done when:** the smoke test passes and exercises every routing and dispatch outcome through one fixture.

## 12. Determinism and "no I/O" guards

- [ ] Add a test in `writ/determinism_test.go` that loads the same fixture into two `Writ` instances and walks both routing tables asserting structural equality: same method ordering, same per-method segment sequences, same span sequence per route, same registered-fn names per `resolveStep`/`formatStep`.
- [ ] Add a no-I/O source-grep test analogous to `pipeline/determinism_test.go`'s `TestNoIO`. The runtime *does* import `os` (for `os.Getenv`) and `net/http`; the grep allowlists `"os"` and `"net/http"` precisely while still catching `time.`, `runtime.NumGoroutine`, and `go func` in non-test files.

**Done when:** both tests pass and the no-I/O assertion mechanism is in the test file as a runtime check.

## 13. README update and markdown lint

- [ ] Update the Features table in `README.md` to set 003's status to `planned`.
- [ ] Run `npx markdownlint-cli2` on every `.md` file under `specs/003-runtime-skeleton/` and on `README.md`. Fix any violations.

**Done when:** markdownlint exits clean on `spec.md`, `plan.md`, `data-model.md`, `tasks.md`, and `README.md`.

## 14. Status transition

- [ ] Confirm with the user that all acceptance criteria are testable as written, the data model is consistent with the spec, and the task ordering matches their judgment.
- [ ] Update `spec.md` status from `clarified` to `planned`. (The plan command transitions to `planned`; the implement command later transitions to `in-progress` then `done`.)

**Done when:** the user has confirmed the plan and the spec status reads `planned`.
