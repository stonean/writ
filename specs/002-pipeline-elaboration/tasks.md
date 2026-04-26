# 002 — Pipeline Elaboration Tasks

Tasks derived from the [plan](plan.md) and [data model](data-model.md). Complete in order. Each task has a clear definition of done; do not move on until that condition is met.

## 1. Package skeleton and error type

- [x] Create `pipeline/doc.go` with the package comment declaring `pipeline` exported but unstable pre-1.0 (matching the AST package's stance per AGENTS.md).
- [x] Create `pipeline/error.go` with the `Error` struct (`File`, `Line`, `Column`, `Span`, `Spans`, `Kind`, `Message`), the `ErrorKind` type and constants (`StagePlacement`, `StageOrder`, `AmbiguousGroup`, `AmbiguousErrorsBlock`), and an `Error() string` method that formats as `"file:line:col: message"`.
- [x] Confirm `go build ./pipeline/...` and `go vet ./pipeline/...` are clean on the empty package.

**Done when:** the package compiles, the error type formats as expected in a unit test, and `go vet` is clean.

## 2. Resolved data model and Stage interface

- [x] Create `pipeline/resolved.go` with `Resolved`, `Handler`, `OptOut`, `ErrorMapEntry`, `StageKind` (with `CanonicalPosition`, `IsObservational`, `IsSingleInstance`, `IsMultiInstance`, `IsTerminal`, `String` methods), and `SourceLevel` (with `String`).
- [x] Create `pipeline/stage.go` with the `Stage` interface and the 12 concrete stage types (`LogStage`, `MeasureStage`, `SessionStage`, `CSRFStage`, `LimitStage`, `ApproveStage`, `ResolveStage`, `CommitStage`, `EmitStage`, `LayoutStage`, `FormatStage`, `RedirectStage`), each with its accessor methods per the data model.
- [x] Implement uniform constructors `newLogStage`, `newMeasureStage`, etc., taking `(src *ast.XxxStmt, level SourceLevel, group *ast.GroupBlock)`.
- [x] Add unit tests asserting each `StageKind` predicate (`IsObservational`, `IsSingleInstance`, `IsMultiInstance`, `IsTerminal`) returns the correct value for every kind, and that `CanonicalPosition` returns the documented values.

**Done when:** every type and method in the data model is declared, the package builds, and the predicate unit tests pass.

## 3. Route-pattern containment helpers

- [x] Create `pipeline/route.go` with `containsAll`, `strictlyContains`, and `equalPatterns` per the plan's containment table.
- [x] Implement segment-level helpers (`segContains`, `splitSegments` to peel off a trailing wildcard) as unexported.
- [x] Create `pipeline/route_test.go` with table-driven tests covering: identical patterns, parameter-name differences, literal-vs-parameter, wildcard vs non-wildcard, wildcard prefixes of differing lengths, and the four cases from the plan's containment table.

**Done when:** `go test ./pipeline -run TestRoute` passes and every row in the plan's containment table has a corresponding test.

## 4. Matching-set construction with ambiguity detection

- [x] Create `pipeline/match.go` with a generic-shaped helper `findKept[T any](candidates []T, patternOf func(T) *ast.RoutePattern, target *ast.RoutePattern) (kept []T, conflicting []T)` that returns the matching containment-chain set and the conflicting set per the plan's algorithm.
- [x] Add `sortBySpecificity[T any](kept []T, patternOf func(T) *ast.RoutePattern, declOrder func(T) int)` that sorts least-specific to most-specific, breaking ties on declaration order.
- [x] Add unit tests in `pipeline/match_test.go` covering: no matches, single match, two-group containment chain, three-group containment chain, two groups overlapping without containment, a chain plus an unrelated overlapping group (chain kept, unrelated group conflicting), equal patterns (treated as containment chain in declaration order).

**Done when:** every matching-set scenario in the spec's *Group Membership* and *Errors Block Selection* sections has a passing unit test.

## 5. Per-level pipeline composer (placement and order checks)

- [x] Create `pipeline/level.go` with a `levelEntry` internal type (kind, source, isNone, attached observationals) and a `composeLevel(stmts []ast.Stmt, level SourceLevel) (entries []levelEntry, errs []Error)` function. (The originating `*ast.GroupBlock` is attached later by the override engine, not at the level-composition stage.)
- [x] In `composeLevel`, run the stage-placement check first: emit a `StagePlacement` error for `format`/`redirect` at non-handler levels and for `format none`/`redirect none` at any level. Statements that fail placement are dropped from the level's entries.
- [x] Run the stage-order check on remaining semantic stages: track the highest canonical position seen and emit a `StageOrder` error for any semantic statement whose canonical position is lower than the highest seen. The offending statement is still placed at its canonical position so consumers can render it.
- [x] Place observational entries at their source positions relative to surrounding semantic entries (the "attached observational" mechanism in the plan).
- [x] Add unit tests in `pipeline/level_test.go` covering: canonical-order handler, non-canonical handler (order error reported but statement still in canonical position), `format` in system block (placement error), `format none` at handler level (placement error), `redirect` in group (placement error), observational stage between two `resolve`s, and an empty level (zero entries).

**Done when:** every stage-placement and stage-order acceptance criterion has at least one corresponding passing test, and `composeLevel` returns deterministic output across repeated calls on the same input.

## 6. Cross-level override engine

- [ ] Create `pipeline/override.go` with `buildEffectiveStages(systemEntries []levelEntry, groupEntries [][]levelEntry, handlerEntries []levelEntry) (stages []Stage, optOuts []OptOut)` per the plan's algorithm.
- [ ] Implement single-instance precedence: latest level wins; `<kind> none` records an opt-out unless overridden by a later level.
- [ ] Implement multi-instance accumulation: cross-level concatenation in precedence order; `<kind> none` clears the accumulated list at and below that level.
- [ ] Implement observational interleaving: each observational entry attaches to the canonical position of the nearest preceding semantic entry within its level; cross-level ordering places system observationals before group observationals before handler observationals at a shared anchor.
- [ ] Add unit tests in `pipeline/override_test.go` covering each row of the spec's *Single-Instance Override*, *Multi-Instance Composition*, and *Nested Group Layering* acceptance criteria, plus the observational interleaving cases (system `log` + handler `log`, handler `log` between two `resolve`s).

**Done when:** every acceptance criterion in the *Single-Instance Override*, *Multi-Instance Composition*, and *Nested Group Layering* spec sections has a passing unit test.

## 7. Effective error map construction

- [ ] Create `pipeline/errors_block.go` with `buildErrorMap(handlerPattern *ast.RoutePattern, blocks []*ast.ErrorsBlock) (entries []ErrorMapEntry, errs []Error)`.
- [ ] Reuse `findKept` and `sortBySpecificity` from `match.go` for the kept-set and ambiguity detection (emit `AmbiguousErrorsBlock` errors with the same shape as `AmbiguousGroup`).
- [ ] Layer kept blocks least-specific to most-specific: for each `TypeName`, the most-specific block's entry wins. Build the result slice by iterating kept blocks and entries in their natural order; track which `TypeName`s have been emitted to skip duplicates from less-specific blocks.
- [ ] Add unit tests in `pipeline/errors_block_test.go` covering: no matching blocks (empty map), single matching block, two blocks in containment (more-specific wins per type, less-specific fills gaps), `default` entry handling, two blocks overlapping without containment (ambiguity error + clean chain falls back to remaining blocks), and source-span preservation per entry.

**Done when:** every acceptance criterion in the *Errors Block Selection* and *Ambiguous Errors Block Membership* sections has a passing unit test.

## 8. Top-level orchestration: `Elaborate`

- [ ] Create `pipeline/elaborate.go` with the `Elaborate(prog *ast.Program) (*Resolved, []Error)` entry point.
- [ ] In `Elaborate`: handle nil/empty program (return a non-nil `Resolved` with zero handlers), build the system level entries once, then per handler: skip partial handler nodes (nil `Pattern` or empty `Method`), find kept groups + conflicting groups, emit any `AmbiguousGroup` error referencing the conflicting groups and the handler block, build per-group level entries, run `buildEffectiveStages`, run `buildErrorMap` against `prog.Errors`, and assemble the `*Handler`.
- [ ] Append all errors (system-level placement/order errors, per-group placement/order errors, per-handler placement/order errors, ambiguity errors) into a single slice in walk order.
- [ ] Always return a non-nil `*Resolved`; on partial failure include every cleanly-resolved handler.
- [ ] Add a smoke test that round-trips a small `.writ` source through `parser.ParseString` then `Elaborate` and asserts handler count, basic stage shape, and zero errors.

**Done when:** the smoke test passes and `Elaborate` produces a non-nil `*Resolved` for every input including an empty program.

## 9. Acceptance criterion test pass

Cover every checkbox under "Acceptance Criteria" in `spec.md`. Group tests by spec section. Use `parser.ParseString` to feed `Elaborate` so tests round-trip through the real parser; use small in-memory `.writ` strings where possible and `pipeline/testdata/*.writ` fixtures where larger sources help readability.

- [ ] **Single-Instance Override** — every checkbox: system-only inheritance, group-only inheritance, handler override, `approve none`, every single-instance kind covered, `layout none` opt-out distinct from default.
- [ ] **Multi-Instance Composition** — every checkbox: system + handler list ordering, three-level layering, two-group containment ordering, source-order preservation within a level, `resolve none` clears inheritance, `commit` and `emit` follow the same rules, observational composition with source-order positioning.
- [ ] **Group Membership** — every checkbox: containment match, non-match, no-group-only-system, parameter-segment containment.
- [ ] **Nested Group Layering** — every checkbox: containment-chain layering, less-specific fills gaps, multi-instance ordering across nested groups.
- [ ] **Errors Block Selection** — every checkbox: layered specificity, most-specific wins per type, no-match empty map, reachable from handler entry, source-span preservation.
- [ ] **Ambiguous Errors Block Membership** — every checkbox: ambiguity error span list, kept chain after ambiguity, empty map when no clean chain remains.
- [ ] **Canonical Stage Order** — every checkbox: canonical-order handler clean, non-canonical handler reports + still resolves, system/group order check, observational exemption, error carries file/line/column/message, offending statement still placed canonically.
- [ ] **Source Provenance** — every checkbox: span references the originating declaration, post-include-flatten spans still point at original files.
- [ ] **Determinism** — same-input → same-output structural equality (covered in task 10), no I/O during elaboration.
- [ ] **Empty / Non-Existent Stages** — every checkbox: absent vs `none` distinction, system-block-only program produces zero handlers, partial handler nodes skipped silently, partial system block treated as absent.
- [ ] **Stage-Placement Errors** — every checkbox: `format` in system, `format` in group, `redirect` in system or group, `format none` and `redirect none` at any level, every error carries file/line/column/message, non-nil `Resolved` on failure, multiple violations in single pass.
- [ ] **Ambiguous Group Membership** — every checkbox: ambiguity error fires, spans list every conflicting group + handler, system-only inheritance for affected handler, three-group chain produces no error, chain plus unrelated overlap reports only the unrelated group.

**Done when:** every acceptance-criteria checkbox in `spec.md` has at least one corresponding passing test, and `go test ./pipeline/... ./ast/... ./parser/...` is green.

## 10. Determinism and "no I/O" guards

- [ ] Add a test that elaborates the same parsed program twice and walks both `*Resolved` values asserting structural equality (handler count, per-handler `Stages` kind+span+source-level sequence, `OptOuts` kind+span sequence, `ErrorMap` entry sequence).
- [ ] Add a test that runs `Elaborate` while a `t.Cleanup`-restored hook (or a build-tag-gated panic) would fire if the package opened files, accessed environment, or started goroutines. Simplest implementation: assert via code review checklist plus a `go vet`-style check that no `os.`, `time.`, `net.`, `runtime.NumGoroutine`, or `go func` appears in the package source. Document the assertion in the test file's comment.

**Done when:** both tests pass and the no-I/O assertion mechanism is in the test file as either a runtime check or a documented source-grep with a CI hook.

## 11. Markdown lint

- [ ] Run `npx markdownlint-cli2` on every `.md` file under `specs/002-pipeline-elaboration/`. Fix any violations.

**Done when:** markdownlint exits clean on `spec.md`, `plan.md`, `data-model.md`, and `tasks.md`.

## 12. Status transition

- [ ] Confirm with the user that all acceptance criteria are testable as written, the data model is consistent with the spec, and the task ordering matches their judgment.
- [ ] Update `spec.md` status from `clarified` to `planned` (the plan command transitions to `planned`; the implement command later transitions to `in-progress` then `done`).

**Done when:** the user has confirmed the plan and the spec status reads `planned`.
