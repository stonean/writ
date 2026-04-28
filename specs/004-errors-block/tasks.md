# 004 — Errors Block Runtime Tasks

Tasks derived from the [plan](plan.md) and [data model](data-model.md). Complete in order. Each task has a clear definition of done; do not move on until that condition is met.

## 1. New `ErrorKind` constants

- [x] Append `KindUnregisteredErrorFormatter` and `KindUnregisteredErrorType` to the existing `ErrorKind` iota block in `writ/error.go`. Append-only ordering preserves spec-003 integer values.
- [x] Extend `ErrorKind.String()` with the two new cases (`"unregistered-error-formatter"`, `"unregistered-error-type"`).
- [x] Add unit tests in `writ/error_test.go` covering the two new `String()` cases.

**Done when:** every existing test in `writ/error_test.go` plus the new cases passes; `go vet ./writ/...` is clean.

## 2. `ErrorData` accessor and `ErrorFormatterFunc`

- [x] Create `writ/error_data.go` with the `ErrorFormatterFunc` type alias and the `ErrorData` struct (unexported `err`, `status`, `request` fields) plus exported `Err()`, `Status()`, and `Request()` accessors per the data model.
- [x] Add unit tests in `writ/error_data_test.go` covering each accessor on a constructed `ErrorData`, including a zero-value case.

**Done when:** unit tests pass and the package builds.

## 3. `ErrorFormatter` registration method and `ErrorType[T]` generic

- [x] Add `errorFormatters map[string]ErrorFormatterFunc` and `errorTypes map[string]func(error) bool` fields to the `Writ` struct in `writ/writ.go`. Initialize both in `New()`.
- [x] Add `(*Writ).ErrorFormatter(name string, fn ErrorFormatterFunc) error` to `writ/register.go`. Lifecycle rules: returns error on duplicate name; panics outside `stateInit`. Reuses the existing `mustBeInit` helper.
- [x] Add `(*Writ).registerErrorType(name string, matcher func(error) bool) error` (unexported) to `writ/register.go`. Same lifecycle rules.
- [x] Create `writ/error_register.go` with `func ErrorType[T error](w *Writ, name string) error` — top-level generic that constructs the `errors.As`-based closure and calls `registerErrorType`.
- [x] Add unit tests in `writ/error_register_test.go` covering: `ErrorFormatter` registers, duplicate-name error, post-`Load` panic; `ErrorType[T]` registers, duplicate-name error, post-`Load` panic; three-namespace independence (the same `name` in success-formatter, error-formatter, and error-type registries does not collide).

**Done when:** every Registration acceptance criterion in `spec.md` has at least one passing test, `go test ./writ/...` is green, and `go vet` is clean.

## 4. Compiled error entries

- [x] In `writ/route.go`, add `compiledErrorEntry` struct and the `errorEntries []compiledErrorEntry` field on `compiledRoute` per the data model.
- [x] Extend `compileHandler` to walk `handler.ErrorMap` and populate `route.errorEntries`. For each `pipeline.ErrorMapEntry`:
  - If `IsDefault` is true: append an entry with `isDefault: true`, `matcher: nil`, formatter pre-resolved from `errorFormatters`.
  - Otherwise: append with the matcher pre-resolved from `errorTypes` (look up by `TypeName`), formatter pre-resolved from `errorFormatters`.
  - If either lookup fails, the validation pass (task 5) emits the appropriate entry and the route is dropped — `compileHandler` returns nil consistent with spec-003's failure mode.
- [x] Extend `writ/route_test.go` with table tests covering: empty `ErrorMap`, one concrete-type entry, one `default` entry, mixed concrete-and-default, and the order-preservation contract (entries appear in `errorEntries` in the same order as `handler.ErrorMap`).

**Done when:** route_test.go's new tests pass and `compiledRoute.errorEntries` is populated correctly for every `pipeline.ErrorMapEntry`.

## 5. Validation pass

- [x] In `writ/validate.go`, add `checkErrorMap(handler *pipeline.Handler, errorTypes, errorFormatters)` that walks `handler.ErrorMap` and emits `KindUnregisteredErrorType` for missing non-default type registrations and `KindUnregisteredErrorFormatter` for missing formatter registrations. The `default` keyword is exempt from the type-registration check.
- [x] Call `checkErrorMap` from the `validate` orchestrator for every handler.
- [x] Add tests in `writ/validate_test.go`: missing error formatter (named); missing error type (named); both missing on the same entry (both reported); `default` entry with missing formatter still flagged; `default` entry with no registered "default" type does NOT flag `KindUnregisteredErrorType`; existing program with no `errors` block produces zero new entries (regression).

**Done when:** every Validation acceptance criterion in `spec.md` has at least one passing test.

## 6. Status resolution and plain-text fallback writer

- [x] Create `writ/error_dispatch.go`. Add `func resolveStatus(err error) (status int, hasStatusCode bool)` that uses an anonymous-interface assertion against `interface{ StatusCode() int }`.
- [x] Add `func writeStatusText(rw http.ResponseWriter, status int)` that writes `Content-Type: text/plain; charset=utf-8`, the status, and `<status> <reason>\n` body using `http.StatusText`. When `http.StatusText` returns empty, use `"Error"` as the reason phrase.
- [x] Add unit tests in `writ/error_dispatch_test.go` covering: `resolveStatus` on a typed error implementing `StatusCode`, on a plain `errors.New`, on `nil` (defensive — should not be called but should not panic if it is); `writeStatusText` for known statuses (200, 404, 422, 500, 503) and an unknown one (e.g., 599).

**Done when:** unit tests pass and `gosec ./writ/...` remains clean.

## 7. Dispatch integration

- [x] In `writ/error_dispatch.go`, add `handleResolverError(rw http.ResponseWriter, req *http.Request, route *compiledRoute, err error)` per the plan: walk `route.errorEntries`, prefer concrete-type matches, fall back to default, fall back to `writeStatusText` (when `hasStatusCode`) or `write500` (otherwise).
- [x] Add `invokeErrorFormatter(rw, req, entry, err, status)` helper that wraps `rw` with `writeRecorder` and applies the same pre-/post-write rules as the success-path formatter. Pass `req.Context()` as the formatter's context.
- [x] In `writ/dispatch.go`, replace the existing `write500(rw)` call after a resolver error with `handleResolverError(rw, req, route, err)`.
- [x] Extend `writ/dispatch_test.go` with end-to-end scenarios via `httptest`: concrete-type match invokes the formatter; `default` match invokes when no concrete entry matches; no-match-with-StatusCode produces the plain-text fallback; no-match-without-StatusCode produces the spec-003 generic 500; error-formatter pre-write error produces the runtime-owned 500; error-formatter post-write error leaves the partial response in flight; `ErrorData.Request()` returns the originating request.

**Done when:** every Type-matching, Error-formatter-dispatch, Status-resolution, and Backward-compatibility acceptance criterion has at least one passing test.

## 8. Acceptance criterion test pass

Cover every checkbox under "Acceptance Criteria" in `spec.md`. Group tests by spec section. Use `parser.ParseString` (or `Load` against a `writeWritFile` fixture) plus `httptest.NewServer` for end-to-end coverage.

- [x] **Registration** — every checkbox: `ErrorFormatter` registers / duplicate / post-Load panic; `ErrorType[T]` registers / duplicate / post-Load panic; three-namespace independence.
- [x] **Validation** — every checkbox: missing error formatter; no-`errors`-block regression; success-formatter-only registration is not enough; missing error type; `default` exempt from type check.
- [x] **Status resolution** — every checkbox: typed error with `StatusCode`; untyped error; status resolution does not run on no-match path.
- [x] **Type matching** — every checkbox: value match, pointer match, wrapped-error match (`fmt.Errorf("...: %w", err)`); concrete-vs-default precedence; default-only error map; empty error map fallthrough.
- [x] **Error formatter dispatch** — every checkbox: `ErrorData` accessors; status passthrough; pre-write error → 500; post-write error preserves partial response; `Request()` accessor.
- [x] **Backward compatibility** — every checkbox: program without `errors` block byte-identical to spec 003; program with unused `ErrorFormatter`/`ErrorType` registrations loads cleanly.
- [x] **Source provenance** — `KindUnregisteredErrorFormatter` carries the originating `errors` entry's span (covered in task 5; restate in `acceptance_test.go` with a parser-driven fixture).
- [x] **Determinism** — covered in task 9.

**Done when:** every acceptance-criteria checkbox in `spec.md` has at least one corresponding passing test, and `go test ./writ/... ./pipeline/... ./parser/... ./ast/...` is green.

## 9. Determinism test extension

- [x] In `writ/determinism_test.go`, extend the structural-equality probe to walk `compiledRoute.errorEntries` per route and assert: same length, same `(typeName, isDefault, span)` tuple at each index. Function pointers (`matcher`, `formatter`) are not compared.
- [x] Use a fixture with `errors /* -> default fmt`, `errors /admin/* -> NotFound nf`, and a handler that triggers both; load it twice into separate `Writ` instances and walk both tables.

**Done when:** the determinism test passes and exercises the new fields.

## 10. Smoke test fixture

- [x] Create `writ/testdata/errors_smoke.writ` with at least one handler that has a populated effective error map (one concrete type plus `default`) and at least one handler with no `errors` block (regression).
- [x] Extend `writ/smoke_test.go` (or add `writ/errors_smoke_test.go`) to load the fixture, register the necessary error formatters and error types, and exercise: a request that triggers a typed-match formatter, a request that triggers the `default` formatter, a request whose handler has no `errors` block (falls through to spec-003 500), and a request that triggers a `StatusCode`-bearing error with no errors-block match (Q3 plain-text fallback).

**Done when:** the smoke test passes and exercises every dispatch outcome the new feature can produce.

## 11. README update and markdown lint

- [x] Update the Features table in `README.md` to set 004's status to `in-progress` (the implement command later transitions to `done`).
- [x] Run `npx markdownlint-cli2` on every `.md` file under `specs/004-errors-block/` and on `README.md`. Fix any violations.

**Done when:** markdownlint exits clean on `spec.md`, `plan.md`, `data-model.md`, `tasks.md`, and `README.md`.

## 12. Status transition

- [x] Walk through each acceptance criterion in `spec.md` and verify it is met. Mark each passing criterion `- [x]`. If any fails, leave it unchecked and report the failure.
- [x] Confirm with the user that the validation gate passes (all tasks marked done, all acceptance criteria marked done, markdownlint clean, full test suite green under `-race`).
- [x] Update `spec.md` status from `in-progress` to `done` after user confirmation.

**Done when:** the user has approved the transition and the spec status reads `done`.
