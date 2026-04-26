# 002 — Pipeline Elaboration Plan

## Overview

Pipeline elaboration is a pure transform from `*ast.Program` to a `*Resolved` value plus a flat slice of structured errors. The implementation is a single-package Go library — `pipeline` — sitting alongside `ast` and `parser`. It performs no I/O, no logging, no goroutine launches, and no map-iteration-order-sensitive work.

The transform is built from four cooperating pieces: a route-pattern containment helper that powers all specificity decisions; a per-handler "matching set" builder that picks groups and `errors` blocks (and detects ambiguity); a per-level pipeline composer that canonicalizes semantic-stage order while letting observational stages float at their source positions; and a cross-level override engine that applies the single-instance / multi-instance / opt-out rules. Stage-placement and stage-order checks run inline as the AST is walked so every violation in a single pass is collected without restarting the walk.

`Resolved` references AST nodes by pointer rather than copying their payloads. The AST is already stable and span-bearing; duplicating call expressions, approve trees, and string fields would bloat the resolved structure for no gain. "Independent of the source program" in the spec means consumers do not need parser session state — they do not lose the right to keep the AST around.

The package is exported but documented as unstable pre-1.0, matching the AST package's stance.

## Technical Decisions

### Single `pipeline` package

The transform has one entry point (`Elaborate`), one returned tree (`*Resolved`), and one error type. All implementation files live in a single `pipeline/` package. There is no need for a sub-package split: the override engine, route helpers, and validation passes are tightly coupled and have no consumer outside the package.

Alternatives considered:

- **`pipeline/elaborate` + `pipeline/resolved`** — would expose the resolved-value types in a smaller surface. Rejected because consumers (runtime, codegen, CLI) want a single import; splitting the resolved types out would either require both packages to be imported together or force a re-export shim.
- **Putting elaboration inside `ast`** — would couple a semantic transform to the syntactic data model. Rejected because the AST is the parser's contract; elaboration is a layer above and should not bleed into AST package symbols.

### Public API

```go
package pipeline

import "github.com/stonean/writ/ast"

// Elaborate resolves a parsed Program into a per-handler effective
// pipeline structure plus a flat list of structured elaboration errors.
// The returned *Resolved is always non-nil; on partial failure it
// contains entries for every handler that elaborated cleanly. The
// returned error slice is the authoritative success indicator.
func Elaborate(prog *ast.Program) (*Resolved, []Error)
```

The signature mirrors `parser.Parse` deliberately: non-nil result, flat error slice, "len(errs) == 0" as the success gate. Callers compose the parser and elaborator naturally:

```go
prog, parseErrs := parser.Parse("app.writ")
resolved, elabErrs := pipeline.Elaborate(prog)
if len(parseErrs) > 0 || len(elabErrs) > 0 { /* report */ }
```

`Elaborate` accepts a partial program. Per the spec's *Inputs* contract, missing/partial nodes are skipped silently — the parser already named the failure location.

### Error type

```go
type Error struct {
    File    string
    Line    int
    Column  int
    Span    ast.Span     // primary span of the violation
    Spans   []ast.Span   // additional spans (ambiguity errors point at every conflicting block + the affected handler)
    Kind    ErrorKind
    Message string
}

func (e Error) Error() string // formats as "file:line:col: message"

type ErrorKind int

const (
    StagePlacement     ErrorKind = iota // format/redirect outside handler, format/redirect none
    StageOrder                          // semantic stage in non-canonical source order
    AmbiguousGroup                      // matching groups overlap without containment
    AmbiguousErrorsBlock                // matching errors blocks overlap without containment
)
```

`Spans` is empty for single-site errors. `Kind` is exported so downstream tooling (LSP, `writ show`) can switch on category without parsing the message string.

Errors are accumulated in a single slice across the whole program. The slice ends up in source-file/line order because the elaborator walks the AST in declaration order; the per-handler loop appends ambiguity errors at the point the handler is processed. Output order is documented as "deterministic given the same input AST" — no specific contract beyond that, matching the parser's stance.

### Route-pattern containment

All specificity decisions reduce to one helper:

```go
// containsAll returns true if every route matched by sub is also
// matched by sup.
func containsAll(sub, sup *ast.RoutePattern) bool
```

Cases:

| sub | sup | Rule |
| --- | --- | --- |
| no wildcard | no wildcard | `len(sub) == len(sup)` and segment-by-segment `sub[i] ⊆ sup[i]` |
| no wildcard | wildcard, prefix N | `len(sub) >= N` and `sub[i] ⊆ sup[i]` for `i < N` |
| wildcard, prefix M | no wildcard | always false (sub matches paths longer than sup ever can) |
| wildcard, prefix M | wildcard, prefix N | `M >= N` and `sub[i] ⊆ sup[i]` for `i < N` |

Segment containment:

| sub segment | sup segment | Result |
| --- | --- | --- |
| literal X | literal X | true |
| literal X | literal Y | false |
| literal X | parameter Y | true (a literal is one specific value; a parameter accepts any) |
| parameter X | parameter Y | true (parameter sets are equal — both match any non-empty segment) |
| parameter X | literal Y | false (parameter accepts values literal Y does not) |

Helpers built on `containsAll`:

- `strictlyContains(a, b *ast.RoutePattern) bool` — `containsAll(b, a) && !containsAll(a, b)` (a is strictly more specific than b).
- `equalPatterns(a, b *ast.RoutePattern) bool` — `containsAll(a, b) && containsAll(b, a)` (same set of paths).

`equalPatterns` is true for syntactically identical patterns and for patterns differing only by parameter name (`/users/:id` ≡ `/users/:user_id`). The override engine treats equal patterns as a containment chain of length one (no ambiguity, identical declarations layer in declaration order). Treating equal patterns as a special "duplicate" diagnostic is out of scope for this spec; startup validation can later add it.

### Matching set + ambiguity detection

For each handler:

1. Walk all `Program.Groups`; collect every group whose pattern contains the handler's pattern (i.e., `containsAll(handler.Pattern, group.Pattern)`).
2. Identify "conflicting" groups — any group `Gi` that has at least one peer `Gj` where neither `containsAll(Gi, Gj)` nor `containsAll(Gj, Gi)` holds. These are the groups whose specificity is undefined relative to a sibling.
3. The "kept" set is matching groups minus conflicting groups. By construction the kept set forms a clean containment chain (every pair is comparable).
4. If the conflicting set is non-empty, emit one `AmbiguousGroup` error whose `Span` is the handler block and whose `Spans` lists every conflicting group's span. Per the spec's "the handler's resolved entry inherits only from the system block (skipping every conflicting group)", the kept set is still used; the spec's *Resolved Questions* example "When a containment chain exists alongside an unrelated overlapping group, only the unrelated group is reported as ambiguous; the chain layers normally" is satisfied because only the overlapping group ends up in the conflicting set.
5. Sort the kept set by specificity, least-specific first. Sorting uses `strictlyContains` as the comparator. When two kept groups have equal patterns, the one declared earlier in the AST sorts first (deterministic tiebreak).

The same algorithm runs for `errors` blocks, producing a kept set of `*ast.ErrorsBlock` and an `AmbiguousErrorsBlock` error when conflicts exist.

### Per-level pipeline composition

Each "level" is one of: the system block (if any), each kept group (in specificity order, least-specific first), and the handler block. For each level we build an ordered list of "level entries":

- A semantic stage statement (single-instance or multi-instance) becomes a level entry tagged with its `StageKind`.
- An observational stage statement becomes a level entry tagged `StageLog`/`StageMeasure`.
- A `<stage> none` becomes a level entry tagged with the cleared kind plus an `IsNone` flag.

Two passes per level:

1. **Stage-order check.** Walk source statements in source order. Track the highest canonical position seen so far among semantic stages. If the next semantic statement's canonical position is less than the highest seen, emit a `StageOrder` error pointing at that statement and naming the canonical position its kind belongs in. The check runs over semantic stages only; observational stages are skipped per the spec's *Canonical Stage Order* carve-out.
2. **Canonical reorder.** Place each semantic stage at its canonical position; observational stages keep their relative position to the surrounding semantic stages. Concretely: process source in order, and for each observational entry, "attach" it to the nearest preceding semantic entry's slot (or to a synthetic "before everything" slot if no semantic entry has been seen yet). When emitting the final per-level list, walk canonical positions in order and emit each canonical position followed by its attached observational entries.

For multi-instance stages within a level (multiple `resolve`s, etc.), source declaration order is preserved within that canonical slot. Observational entries between two source-adjacent multi-instance entries attach to the one they follow in source.

The result: per-level lists of `levelEntry{kind, ...}` in canonical order with observational floats correctly placed.

### Cross-level override engine

After per-level lists are built, walk levels in precedence order (system → kept groups least-to-most-specific → handler) and apply override rules:

For **single-instance** semantic stages (`session`, `csrf`, `limit`, `approve`, `layout`, `format`, `redirect`):

- Track a "current value" per kind. Each level can replace it with a new value or clear it via `<kind> none` (which also records an opt-out at this level).
- After all levels are processed, the final value (if any) wins. The opt-out marker is the most recent `none` at any level — if a later level declares a value after a `none`, the value wins and the opt-out is cleared.

Wait — the override semantics need clarification. The spec's *None Semantics* section: "`approve none` at handler level means the handler has no `approve` stage even if system or group declared one." So `none` removes inherited declarations for the rest of the precedence walk. But what if handler has `approve none` followed by no `approve`? Then no `approve`. What if handler has `approve none` followed by another `approve auth.X`? The DSL grammar permits multiple statements per block; semantically the second `approve` redeclares for this level. Treat the latest declaration in source order as authoritative for the level. This is consistent with the parser preserving declaration order and with the "stages must appear in canonical order" check (which would already complain about two `approve` lines because the second is "before" the first canonically — actually no, both are at the same canonical position, so no order violation). For the elaborator: within a level, the latest declaration of a single-instance stage wins for that level. The override engine then applies cross-level precedence using each level's final value.

For **multi-instance** semantic stages (`resolve`, `commit`, `emit`):

- Maintain an accumulating list per kind. Each level appends its declared steps in source order.
- A `<kind> none` at any level clears the entire accumulated list at that level (per the spec: "clears all inherited steps at and below that level"). Subsequent declarations at the same level append fresh.

For **observational** stages (`log`, `measure`):

- Accumulate as a list of `(level, sourcePosition, value)` entries. `<kind> none` clears all earlier entries (across levels) and records an opt-out.

After cross-level processing, assemble the effective stage list:

- For each canonical position (in canonical order): emit the surviving single-instance entry (if any) followed by any observational entries that floated to that position. Multi-instance positions emit their accumulated list in cross-level order, with observational entries from each level interleaved at the source positions they originally occupied within their level.

Observational position resolution across levels is the subtle bit. The chosen rule:

- An observational entry at level L "belongs" to the canonical position of the nearest preceding semantic entry within level L (or to a "level start" position if no preceding semantic entry exists).
- When assembling the effective list, observational entries from level L appear in the effective list at the canonical position they belong to, in cross-level order (so a system-level observational that floats to "before approve" appears before all group-level and handler-level observationals that also float to "before approve").

This rule produces the spec's example output (a handler-level `log` between two `resolve`s appears between them) and degrades sensibly across levels (a system-level `log start` appears before any handler-level statements).

### Resolved data model

See `data-model.md`. Highlights:

- `Resolved.Handlers []*Handler`. No global lookup index; `writ routes` and `writ show` walk the slice. (Adding an indexed lookup is additive and can land when a consumer needs it.)
- `Handler.Stages []Stage` — flat, canonical-order, observational-interleaved.
- `Handler.OptOuts []OptOut` — separate slice so `Stages` only contains live stages. Distinguishes "explicitly opted out at the winning level" from "never declared".
- `Handler.ErrorMap []ErrorMapEntry` — slice rather than map so iteration order is deterministic. Lookup is linear; the entry count per handler is small.
- `Stage` is an interface implemented by 12 concrete types (`LogStage`, `MeasureStage`, …, `RedirectStage`). Each concrete type carries a pointer to the originating AST statement and a `Source` field naming the level (`SourceSystem`, `SourceGroup`, `SourceHandler`) plus the originating `*ast.GroupBlock` when source is `SourceGroup` (nil otherwise). The pointer-to-AST means accessors like `LogStage.Args()` thin-wrap `LogStage.src.Args` — no copying, no synthetic node construction.
- Spans on every entry come from the originating AST node, satisfying the spec's "span points at the system block's `approve` line — not at the handler block" requirement automatically.

### Determinism

The implementation avoids any source of non-determinism:

- No `map` ranges in the matching-set, ordering, or override-engine paths. The only `map` use is per-handler error-type-name → entry inside `buildErrorMap`, and that result is materialized into a slice before exposing via `Handler.ErrorMap`. The slice's order is the order the entries are first encountered while walking blocks in specificity order, then declaration order within a block.
- No goroutines.
- No clock, no environment access.
- AST traversal walks `Program.Groups`, `Program.Errors`, and `Program.Handlers` in their slice order, which the parser already documents as declaration order.

A determinism test parses a fixture twice and asserts the two `*Resolved` values are structurally equal (helper walks both trees and compares spans by `(Path, Line, Column, Offset)`).

### Package layout

```text
pipeline/
  doc.go              # package doc — exported but unstable pre-1.0
  elaborate.go        # Elaborate entry point, top-level orchestration
  resolved.go         # Resolved, Handler, OptOut, ErrorMapEntry, StageKind
  stage.go            # Stage interface, 12 concrete stage types, accessors
  error.go            # Error, ErrorKind
  route.go            # containsAll, strictlyContains, equalPatterns + segment helpers
  match.go            # matching-set builder for groups + errors blocks (shared ambiguity logic)
  level.go            # per-level composer (stage-order check + canonical reorder)
  override.go         # cross-level override engine assembling final Stage list
  errors_block.go     # effective ErrorMap construction
  elaborate_test.go   # acceptance tests, table-driven, grouped by spec section
  route_test.go       # containsAll + strictlyContains unit tests
  level_test.go       # per-level composition + stage-order tests
  override_test.go    # cross-level override + observational interleaving tests
  testdata/           # .writ fixtures used by acceptance tests
```

The source-file split is by responsibility, not by node kind. There are no exported types in any file other than `resolved.go`, `stage.go`, `error.go`, and `elaborate.go`.

## Affected Files

| File | Action | Purpose |
| --- | --- | --- |
| `pipeline/doc.go` | Create | Package doc declaring the package exported but unstable pre-1.0 |
| `pipeline/elaborate.go` | Create | `Elaborate` entry point and top-level orchestration |
| `pipeline/resolved.go` | Create | `Resolved`, `Handler`, `OptOut`, `ErrorMapEntry`, `StageKind`, `SourceLevel` |
| `pipeline/stage.go` | Create | `Stage` interface + 12 concrete stage types and accessors |
| `pipeline/error.go` | Create | `Error`, `ErrorKind`, `Error.Error()` |
| `pipeline/route.go` | Create | `containsAll`, `strictlyContains`, `equalPatterns` + segment helpers |
| `pipeline/match.go` | Create | Per-handler matching-set construction with ambiguity detection (shared by groups + errors blocks) |
| `pipeline/level.go` | Create | Per-level pipeline composer: stage-placement check, stage-order check, canonical reorder with observational floats |
| `pipeline/override.go` | Create | Cross-level override engine producing the effective `Stage` list and `OptOut` slice |
| `pipeline/errors_block.go` | Create | Effective `ErrorMap` construction from kept errors blocks |
| `pipeline/elaborate_test.go` | Create | Acceptance tests grouped by spec section |
| `pipeline/route_test.go` | Create | Containment helper unit tests |
| `pipeline/level_test.go` | Create | Per-level composition unit tests (canonical reorder + observational floats) |
| `pipeline/override_test.go` | Create | Cross-level override and observational-interleaving unit tests |
| `pipeline/testdata/` | Create | `.writ` fixtures used by acceptance tests |

The companion `data-model.md` defines the full resolved-value shape.

## Trade-offs

### Considered and rejected

- **Indexed handler lookup (`Resolved.Handler(method, path)`)** — would let runtime/CLI consumers find a handler by route in O(1). Rejected for now: the spec lists `Handlers` as walkable in any order; runtime matches by route at request time using its own data structures, and CLI consumers walk all handlers anyway. Adding a method later is additive and doesn't break the slice contract.
- **Copying call expressions and approve trees into the resolved value** — would make `Resolved` independent of `*ast.Program` even at the pointer level. Rejected: the AST is already the contract for these shapes, copying duplicates 80% of the AST in memory, and consumers gain nothing — they still read the same field shapes. The spec's "independent of the source program after construction" is satisfied because the consumer needs no parser session, just the AST values it already has.
- **Stage-kind enum on the `Stage` interface as a method (`Kind() StageKind`)** — would let consumers switch on `stage.Kind()` without a type switch. Considered and adopted (it's in the data model); the alternative of pure type switches is clean Go but loses the categorical "is this observational?" check that the override engine and `writ show` benefit from.
- **`map[string]Formatter` for the effective error map** — would give O(1) lookup. Rejected because map iteration is non-deterministic, the spec requires deterministic output, and per-handler error-map size is small (typically <10 entries) so linear lookup is fine.
- **A `Resolution Pass` interface to make stage-order, stage-placement, group-ambiguity, and errors-ambiguity each their own pluggable pass** — would map nicely to the four error categories. Rejected as over-engineering: there is exactly one `Elaborate` and these "passes" coordinate enough that a single orchestrator with helper functions is clearer.
- **Erroring on equal-pattern groups (e.g., two `group /admin/*` blocks)** — would be a useful diagnostic. Rejected for this spec: the spec does not list "duplicate group pattern" as an error category, and it isn't obviously a violation — both groups simply layer in declaration order. Startup validation may add it later.
- **A separate `pipeline/testdata` builder package for synthesizing AST fixtures programmatically** — would let acceptance tests avoid round-tripping through `parser.Parse`. Rejected: round-tripping through the parser is the test discipline that ensures the elaborator only assumes what the parser actually produces, and the fixture cost is a few `ParseString` calls per test.

### Known limitations

- **No incremental re-elaboration** — every `Elaborate` call walks the full AST. An LSP that wants to react to a single edit can either re-elaborate (cheap for projects of normal size) or wrap the elaborator with its own caching. Adding incremental support is additive when the LSP spec lands.
- **Equal-pattern groups silently layer in declaration order** — two `group /admin/*` blocks both contribute their statements; later one's single-instance overrides win. Not flagged as ambiguity (both are in containment with each other). A future startup-validation feature can warn.
- **Equal-pattern errors blocks similarly layer** — same trade-off.
- **Stage-placement errors are reported once per offending statement, not per affected handler** — when the system block has `format X`, exactly one `StagePlacement` error is emitted, not one per handler that would have inherited it. This matches "elaboration does not abort on the first error; it collects every violation in one pass" while avoiding O(handlers) duplicate noise.
- **Per-level stage-order check fires once per level per offending statement** — a system block with `approve` before `csrf` produces one `StageOrder` error, not one per handler that inherits the system block. Same noise-reduction rationale.
- **No "this group never matched any handler" warning** — orphan groups silently contribute nothing. Useful as a future startup-validation diagnostic but out of scope here (the spec does not mention it).
- **No structured warnings** — only errors. Matches the parser's stance.

## Open Questions Resolved

The spec's *Open Questions* section reads "*All open questions resolved.*" and the *Resolved Questions* section enumerates twelve previously-decided items, all of which this plan honors:

- **Multi-instance composition order** — system → kept groups (least → most specific) → handler; source order preserved within a level. Implemented in `override.go`.
- **Inherited resolve visibility** — flat list, name-resolution out of scope. Implemented by exposing `ResolveStage.Name()` and emitting all steps regardless of where their referents live.
- **Terminal stages at non-handler levels** — `format`/`redirect` in `system` or `group` produces a `StagePlacement` error. Implemented in `level.go`.
- **`format` line composition** — handler-only; multiple `format` lines preserved as ordered list. Implemented as multi-instance handler-only treatment in `override.go`.
- **Group nesting via overlap (containment chain)** — kept-set sort by specificity. Implemented in `match.go`.
- **Group overlap without containment** — `AmbiguousGroup` error; conflicting groups skipped, system-only inheritance for the handler. Implemented in `match.go`.
- **Errors block specificity ranking** — same containment model as groups. Implemented in `match.go` with shared logic.
- **`none` for terminal stages** — `format none` and `redirect none` produce `StagePlacement` errors at any level. Implemented in `level.go`.
- **`layout none`** — valid composition opt-out; recorded in `OptOuts`. Implemented in `override.go`.
- **Stage interleaving in a level** — canonical-order check for semantic stages; observationals exempt. Implemented in `level.go`.
- **Resolution against partial ASTs** — best-effort; partial nodes skipped silently. Implemented by guarding nil checks in the AST walks (e.g., `HandlerBlock.Pattern == nil` skips).
- **AST stability** — `pipeline` package documented as exported-but-unstable in `doc.go`, matching the AST package.
