---
status: done
dependencies: [001-dsl-parser]
tags: []
---

# 002 — Pipeline Elaboration

Pipeline elaboration is the second layer of meaning on top of the syntactic AST. It walks a parsed program and produces a *resolved structure*: for each handler, the effective pipeline with system-block defaults applied, group-level overrides applied, and handler-level overrides applied — with `none` opt-outs honored, the matching `errors` block chosen, and the source span of every effective stage preserved.

This transform is the contract every downstream feature stands on. The runtime needs it to know what stages to run for an incoming request. Code generation needs it to emit typed glue per handler. The `writ show <route>` and `writ routes` CLIs render it directly. A change to its output shape breaks every consumer, so its contract — what it accepts as input, what shape it produces, what semantics it enforces — needs to be pinned down before any consumer is built.

Pipeline elaboration is structurally focused — it enforces *stage-placement* rules (e.g., `format` and `redirect` only at handler level) and reports any violations as structured errors alongside the resolved value (see *Elaboration Errors*). It does not validate that referenced names resolve to registered Go symbols, that field references match return types, or that cross-stage pairing rules hold (CSRF needs session, etc.) — those belong to startup validation. It performs no I/O.

## Inputs

A parsed program (the AST emitted by [`001-dsl-parser`](../001-dsl-parser/spec.md)). Pipeline elaboration accepts any AST the parser returns — including partial ASTs from a parser-error pass — and silently skips any node the parser left partial. The parser already reported why those nodes are partial; elaboration does not duplicate the diagnosis. Consumers check `len(parserErrs) == 0` *and* `len(elaborationErrs) == 0` to determine overall success.

## Outputs

A resolved value with one entry per handler. Each handler entry carries:

- The route pattern and HTTP method exactly as parsed.
- The handler's *effective stage list* — a list of resolved stages in canonical pipeline order. Semantic stages appear in fixed sequence (`session`, `csrf`, `limit`, `approve`, `resolve`, `commit`, `emit`, `layout`, `format`/`redirect`); observational stages (`log`, `measure`) appear at the positions their source declarations occupy. See *Canonical Stage Order*.
- For each effective stage entry: the resolved value (e.g., the call expression for `approve`, the storage choice for `session`) and the source span of the originating AST node.
- The handler's *effective error map* — a mapping from Go error type name to formatter, accumulated from every matching `errors` block layered by specificity. Each entry carries the source span of the originating block entry. The map is empty when no `errors` block matches.
- The effective `layout` value (or none).
- An explicit-opt-out marker on stages where `none` was declared at the level that effectively wins, distinguishing "stage not present" from "stage explicitly removed."

The resolved value is a single in-memory structure, walkable in any order. It is independent of the source program after construction — consumers do not need to keep the parser session open.

Alongside the resolved value, pipeline elaboration emits a list of structured *elaboration errors* for any stage-placement violations encountered (see *Elaboration Errors*). Like the parser, elaboration always returns a resolved value — partial when errors occur — and the error list is the authoritative success indicator.

## Override Model

For every stage type, the **most specific declaration wins**:

- Handler-level overrides group-level.
- Group-level overrides system-level.
- `none` at any level removes the inherited stage at and below that level.

A handler's effective stage list is built by walking the precedence ladder (system → matching groups → handler) and replacing each stage as more-specific declarations appear. When multiple groups match a handler, all of them contribute — ordered by specificity, least-specific first. Stages with no declaration at any level are absent from the resolved entry.

Specificity between two groups is set inclusion on the route language: group A is more specific than group B when every route matched by A is also matched by B, but not vice versa. `/admin/users/*` is more specific than `/admin/*` because every `/admin/users/...` route is also an `/admin/...` route, but not the other way around. The case where two groups match the same handler without either being more specific than the other is covered separately (see *Group Membership* below).

### Single-Instance Stages

`session`, `csrf`, `limit`, `approve`, and `layout` are single-instance — at most one declaration per level. The most specific level's declaration wins outright; lower-level declarations are discarded.

### Multi-Instance Stages

`resolve`, `commit`, and `emit` allow multiple declarations at the same level, each contributing a step. The effective list for a handler is the concatenation of all declared steps in precedence order: system steps first, then steps from each matching group (less-specific groups before more-specific ones), then handler steps. Within a level, source declaration order is preserved. `<stage> none` at any level clears all inherited steps at and below that level for that stage, providing the explicit escape.

### Observational Stages

`log` and `measure` are **observational** — they record and instrument without gating, mutating, or shaping the response. They are multi-instance (multiple declarations per level are allowed) and may appear at any point in source order; their position in the source determines their position in the effective pipeline. Cross-level composition uses the same rule as multi-instance stages: system → matching groups → handler, source order preserved within each level. `log none` and `measure none` at any level clear all inherited observational steps for that stage at and below that level.

### Terminal Stages

`format` and `redirect` are **terminators** — they end the pipeline rather than compose into it. They are valid only at handler level: they reference `resolve`/`commit` names from the handler's own data, which system and group blocks have no access to, and shared error formatting is already covered by the `errors` block. Their appearance at `system` or `group` level is a stage-placement violation reported as an elaboration error (see *Elaboration Errors*). Multiple `format` lines at the handler level remain an ordered list for content negotiation, preserved as the parser produced them.

### `none` Semantics

`none` is meaningful only for **composition stages** — those that compose into the pipeline and can be inherited from outer scopes. `approve none` at handler level means the handler has no `approve` stage even if system or group declared one. The resolved entry must record this distinctly so consumers can tell "no `approve` was ever declared" from "`approve` was explicitly opted out." For multi-instance stages (`resolve`, `commit`, `emit`), `<stage> none` at any level clears all inherited steps at and below that level — see *Multi-Instance Stages* above.

Terminators (`format`, `redirect`) do not support `none`. They are not composition stages — there is nothing to inherit and nothing to clear, and a handler with `format none` as its only terminal declaration would have no way to terminate. Pipeline elaboration treats `format none` and `redirect none` as stage-placement errors regardless of where they appear.

## Canonical Stage Order

Stages within any block (system, group, or handler) must appear in canonical pipeline order in the source. Pipeline elaboration emits a *stage-order violation* error when source order departs from canonical order.

The canonical sequence for semantic stages:

1. `session`
2. `csrf`
3. `limit`
4. `approve`
5. `resolve` (multi-instance — source order within this kind is preserved)
6. `commit` (multi-instance — source order within this kind is preserved)
7. `emit` (multi-instance — source order within this kind is preserved)
8. `layout`
9. `format` / `redirect` (terminal — multiple `format` lines for content negotiation appear contiguously at the end)

**Observational stages** (`log`, `measure`) are exempt from canonical order. They may appear at any point in source order, multi-instance, with source order preserved within each kind. Their position in the source determines their position in the resolved entry's effective stage list, so a `log` written between two `resolve` lines runs between them.

The principle: source should reflect execution. Semantic stages whose ordering changes behavior must be written in the order they execute; observational stages, which don't change behavior, are written at the position the developer wants them to fire.

Example that elaborates without a stage-order violation:

```text
GET /users/:id ->
  log request_received
  approve auth.isOwner(:id)
  resolve user = db.users(:id)
  log user_loaded
  resolve posts = db.posts(:user.id)
  log posts_loaded
  format user.show.json with user, posts
```

## Group Membership

A handler is a member of a group when the group's route pattern is a route-pattern containment match for the handler's route — using the same pattern grammar the parser produced (literal segments, parameter segments, wildcards), not raw string prefix.

Examples:

- `group /admin/*` matches every handler whose route starts with `/admin/`.
- `group /users/:id/*` matches every handler under `/users/<param>/`.
- A handler not matched by any group inherits only from the system block.

A handler may be matched by multiple groups. In that case, the matching groups must form a containment chain — each pair must have one strictly contained in the other. The handler's effective pipeline accumulates declarations from every matching group in specificity order (least-specific first), per the override model above.

When two matching groups overlap *without* containment — neither pattern is a strict subset of the other, yet both match the handler — the handler has **ambiguous group membership**. Pipeline elaboration emits a structured error and the handler's resolved entry inherits only from the system block (skipping all conflicting groups), so tooling can still report on what is there.

## Errors Block Selection

A handler's effective error map is built by layering every `errors` block whose route pattern matches the handler's route, in specificity order (least-specific first, most-specific last). For each Go error type name, the most-specific matching block's entry wins; less-specific blocks fill in entries the more-specific blocks did not declare. Specificity is set inclusion on the route language — the same definition used for groups (see *Override Model*).

When two matching `errors` blocks overlap *without* containment — neither pattern is a strict subset of the other, yet both match the handler — the handler has **ambiguous errors block membership**. Pipeline elaboration emits a structured error and the handler's effective error map is built from the remaining matching blocks that form a clean containment chain; the conflicting blocks are skipped. If no clean chain exists, the effective error map is empty.

If no `errors` block matches, the handler's effective error map is empty and the runtime falls back to its default error formatting (defined in the runtime spec).

## Source Provenance

Every resolved stage entry carries the span of the AST node it was inherited from. When a handler inherits `approve` from the system block, the resolved entry's span points at the system block's `approve` line — not at the handler block. This makes diagnostics, `writ show <route>`, and `writ routes` able to point a developer back at the file and line that determines current behavior.

After include flattening (per spec 001), spans still reference the originating file. Pipeline elaboration preserves that property.

## Determinism

Pipeline elaboration is a pure function: structurally equivalent input produces structurally equivalent output. No map-iteration-order dependence, no time- or environment-based variation, no concurrency-induced order differences. The transform performs no I/O.

## Elaboration Errors

Pipeline elaboration emits structured errors for two categories of violation.

**Stage-placement violations** — declarations the grammar accepts but elaboration's placement rules forbid:

- A `format` declaration in a `system` or `group` block.
- A `redirect` declaration in a `system` or `group` block.
- A `format none` or `redirect none` declaration at any level (terminators do not support `none`).

**Stage-order violations** — semantic stages within a block in non-canonical order. The error names the offending statement and the canonical position its kind belongs in. The resolved entry still includes the statement, placed in canonical position, so tooling can render the resolved pipeline.

**Ambiguous group membership** — when a handler is matched by two or more groups whose patterns overlap without one containing the other, leaving no rule to determine which group's overrides apply. The handler's resolved entry is built using only system-level inheritance (skipping every conflicting group) so tooling can still render something.

**Ambiguous errors block membership** — when a handler is matched by two or more `errors` blocks whose patterns overlap without one containing the other, leaving no rule to determine which block's entries take precedence. The handler's effective error map is built from the remaining matching blocks that form a clean containment chain; the conflicting blocks are skipped.

Every elaboration error carries:

- The file path, line number, and column number of the offending span (or spans, for ambiguity errors that point at multiple groups and the affected handler).
- A human-readable message naming the rule that was violated.

Elaboration does not abort on the first error; it collects every violation in one pass. The resolved value remains best-effort — handler entries are constructed for every handler that elaborates cleanly, regardless of errors elsewhere in the program.

Elaboration does not perform name resolution, type checking, or cross-stage pairing checks. Errors of those kinds belong to startup validation.

## Out of Scope

The following belong to later features and are explicitly **not** the responsibility of pipeline elaboration:

- Verifying that names referenced in resolved stages (resolvers, approvers, formatters, error types, layouts, templates, body/query types, event listeners) are registered (→ startup validation).
- Verifying that route parameters referenced in calls (`:id`) are present in the route definition (→ startup validation).
- Verifying that field references (`:user.id`) match the return type of a previous step (→ startup validation + code generation).
- Pairing rules: a handler must end in `format` or `redirect`; `using layout` requires `.html`; `csrf auto` requires `session`; `format` and `redirect` cannot be mixed except for content negotiation (→ startup validation).
- Detecting `resolve`/`commit` dependency cycles within a handler (→ runtime or startup validation).
- Matching incoming HTTP requests to a handler at runtime (→ runtime).
- Loading or rendering templates, executing SQL, or invoking Go functions (→ runtime, data layer, HTML rendering).

## Public Go API (Contract Sketch)

Per project convention (see `AGENTS.md` *Spec Conventions*), the intended Go-side shape is sketched here as a contract illustration; exact spellings are refined in the plan phase.

A consumer elaborates a parsed program:

```go
elaborated, errs := pipeline.Elaborate(prog)
```

`errs` is empty on success. On failure, `elaborated` may still contain handler entries for every handler that elaborated cleanly, so that tooling can report on whatever resolved.

The resolved value is walkable. For each handler, a consumer can iterate the effective stage list, retrieving each stage's kind, resolved value, and source span. It can also look up the chosen errors block, the effective layout, and other handler-level metadata.

These spellings are illustrative. Plan phase resolves the exact package, type, and function shapes. The resolved value's package is exported but documented as unstable pre-1.0, matching the AST package's stance.

## Acceptance Criteria

### Single-Instance Override

- [x] When system declares `approve auth.X` and a handler does not declare `approve`, the handler's effective `approve` is `auth.X` and its span points at the system block's declaration.
- [x] When group declares `approve auth.Y` and a handler in that group does not declare `approve`, the handler's effective `approve` is `auth.Y` and its span points at the group's declaration.
- [x] When a handler declares `approve auth.Z`, the handler's effective `approve` is `auth.Z` regardless of system or group declarations, and its span points at the handler's declaration.
- [x] When a handler declares `approve none`, the handler has no effective `approve` stage, and the resolved entry records the explicit opt-out distinctly from "no `approve` declared at any level."
- [x] The same override semantics hold for every single-instance stage: `log`, `measure`, `session`, `csrf`, `limit`, `approve`, `layout`.
- [x] When a handler declares `layout none`, the resolved entry records an explicit opt-out distinct from "no layout declared at any level," so consumers can render without any layout wrapper instead of falling back to the runtime default.

### Multi-Instance Composition

- [x] When system declares `resolve a = X.foo()` and a handler declares `resolve b = X.bar()`, the handler's effective resolve list is `[a, b]` in that order.
- [x] When system, a matching group, and a handler each declare a `resolve` step, the handler's effective resolve list is `[system_step, group_step, handler_step]` (system first, group middle, handler last).
- [x] When two matching groups both declare `resolve` steps and one group's pattern is contained within the other's, the more-specific group's steps appear after the less-specific group's steps.
- [x] Within a single level, multiple declarations of the same multi-instance stage appear in the resolved entry in source declaration order.
- [x] When a handler declares `resolve none`, all inherited `resolve` steps from system and matching groups are cleared, and the handler's effective resolve list contains only the handler's own `resolve` steps (if any).
- [x] The same composition rules apply to `commit` and `emit`.
- [x] The same composition rules apply to observational stages (`log` and `measure`), with one exception: source order across the entire block determines their position in the effective stage list, not their position relative to canonical pipeline ordering.

### Group Membership

- [x] A handler at route `/admin/users/:id` is a member of group `/admin/*` and inherits its overrides.
- [x] A handler at route `/users/:id` is not a member of group `/admin/*` and inherits nothing from it.
- [x] A handler matched by no group inherits only from the system block.
- [x] A handler at route `/users/:id/posts` is a member of group `/users/:id/*`, where `:id` matches any value at that segment position.

### Nested Group Layering

- [x] When two groups match a handler and one is strictly contained in the other (e.g., `/admin/*` contains `/admin/users/*`), the handler inherits from both, with the more specific group's declarations overriding the less specific group's per single-instance stage.
- [x] When the more-specific matching group does not declare a stage that the less-specific matching group declared, the less-specific group's declaration is preserved in the effective pipeline.
- [x] Multi-instance steps from the less-specific matching group precede multi-instance steps from the more-specific matching group in the handler's effective list, before handler-level steps.

### Errors Block Selection

- [x] When a handler at route `/admin/users/:id` matches both `errors /admin/* ->` and `errors /* ->`, its effective error map layers the two: entries from `/admin/*` take precedence; `/*` entries fill in any Go error type names that `/admin/*` did not declare.
- [x] When the same Go error type name appears in two matching `errors` blocks, the more-specific block's entry wins in the effective error map.
- [x] When a handler is matched by no `errors` block, its effective error map is empty.
- [x] Effective error map entries are reachable from the resolved handler entry without re-walking the AST.
- [x] Each effective error map entry carries the source span of the originating `errors` block entry.

### Ambiguous Errors Block Membership

- [x] When two `errors` blocks whose patterns overlap without containment both match a handler, elaboration emits an *ambiguous errors block membership* error referencing every conflicting block's span and the affected handler.
- [x] After the ambiguity error, the handler's effective error map is built from the remaining matching blocks that form a clean containment chain; the conflicting blocks are skipped.
- [x] When no clean containment chain remains after skipping conflicting blocks, the effective error map is empty.

### Canonical Stage Order

- [x] A handler block where semantic stages appear in canonical pipeline order produces no stage-order violation.
- [x] A handler block where `approve` appears before `csrf` (or any non-canonical sequence among semantic stages) produces a stage-order violation error pointing at the offending statement.
- [x] A system or group block with semantic stages in non-canonical order produces a stage-order violation error pointing at the offending statement.
- [x] A handler block with `log` or `measure` interleaved between semantic stages (e.g., `log` after `resolve`, `measure` after `commit`) produces no stage-order violation; observational stages are exempt from canonical order.
- [x] Each stage-order violation error carries a file path, line number, column number, and a human-readable message naming the canonical position the offending stage kind belongs in.
- [x] After a stage-order violation, the resolved entry's effective stage list still contains the offending statement, placed in its canonical position so consumers can render the resolved pipeline.

### Source Provenance

- [x] Every effective stage entry carries a span. The span references the originating file and line of the declaration that won, not the handler's own location when the stage was inherited.
- [x] After include flattening, spans still reference the original file each declaration was written in (not the post-flatten root file position).

### Determinism

- [x] Resolving the same parsed program twice produces structurally equal resolved values: the same handler order, the same canonical stage order within each handler, and equal spans.
- [x] Pipeline elaboration performs no I/O (no file reads, no network, no environment access, no logging).

### Empty / Non-Existent Stages

- [x] A handler whose effective pipeline declares no `approve` at any level has no `approve` entry in the resolved value. This is distinct from a handler whose effective pipeline ends with `approve none`.
- [x] A program with a `system` block but no handlers produces a resolved value with zero handler entries (not an error).
- [x] A parser-error AST containing both well-formed and partial handler nodes produces a resolved value with entries for every well-formed handler; partial handler nodes are skipped silently and do not generate additional elaboration errors.
- [x] A parser-error AST with a partial `system` block is treated as if no system block exists; handlers inherit only from any well-formed matching groups.

### Stage-Placement Errors

- [x] A `format` declaration in a `system` block produces an elaboration error pointing at the `format` line's span. The resolved value still contains entries for every handler that elaborated cleanly.
- [x] A `format` declaration in a `group` block produces an elaboration error pointing at the `format` line's span.
- [x] A `redirect` declaration in a `system` or `group` block produces an elaboration error pointing at the `redirect` line's span.
- [x] A `format none` declaration at any level (system, group, or handler) produces an elaboration error.
- [x] A `redirect none` declaration at any level produces an elaboration error.
- [x] Every elaboration error carries a file path, line number, column number, and a human-readable message.
- [x] Pipeline elaboration always returns a non-nil resolved value; on errors, the resolved value contains every handler entry that elaborated cleanly, and the returned error list is the authoritative success indicator.
- [x] Elaboration reports multiple stage-placement violations in a single pass — it does not abort on the first error.

### Ambiguous Group Membership

- [x] When two groups whose patterns overlap without containment both match a handler (e.g., `group /admin/*` and `group /:tenant/users/*` both matching `GET /admin/users/:id`), elaboration emits an *ambiguous group membership* error.
- [x] The error references the spans of every conflicting group and the affected handler, so tooling can point at all relevant locations.
- [x] The handler's resolved entry inherits only from the system block; every conflicting group is skipped.
- [x] When three or more matching groups form a containment chain, elaboration does not emit an ambiguity error — the layering rule applies normally.
- [x] When a containment chain exists alongside an unrelated overlapping group, only the unrelated group is reported as ambiguous; the chain layers normally for handlers it covers.

## Open Questions

*All open questions resolved.*

## Resolved Questions

- **Multi-instance stage composition** — Inherited multi-instance steps (`resolve`, `commit`, `emit`) execute first in their original declaration order; handler-level steps execute after. Cross-level ordering is system → matching groups (less-specific groups before more-specific ones) → handler. Within a level, source declaration order is preserved (the parser already records it). `<stage> none` at any level clears all inherited steps at and below that level for that stage. Rationale: matches the "stages of a pipeline" mental model and the precedent in similar frameworks (Rails before/after actions, Express middleware) where steps accumulate down the chain; supports useful system-wide patterns (system-level audit `emit`, system-level `current_user` resolve) without per-handler repetition; `<stage> none` provides the escape valve for handlers that genuinely need to opt out.
- **Inherited `resolve` visibility** — Cross-level references are valid: a handler-level `resolve` may reference any inherited `resolve` by name (e.g., `:user.id` where `user` was declared at system or group level), and a group-level `resolve` may reference any system-level `resolve`. This falls out of the multi-instance composition rule above — all `resolve` steps form a single ordered effective list per handler, and `:name.field` references are level-agnostic by design. Pipeline elaboration exposes the flat list; verifying that the referenced name actually exists at the referencing point belongs to startup validation (see *Out of Scope*). Rationale: forbidding cross-level reference would defeat the purpose of system-level resolves like `current_user = auth.user()`, which exist precisely so handlers can reference them without redeclaration.
- **Terminal stages at non-handler levels** — `format` and `redirect` are valid only at handler level. Pipeline elaboration treats their appearance at `system` or `group` level as a structured elaboration error reported alongside the resolved value, parser-style. Rationale: terminal stages reference `resolve`/`commit` names from the handler's own data, which system and group blocks have no access to; shared error formatting is already covered by the `errors` block. `layout`, by contrast, *is* settable at any level because it is a setting (which template wraps the response), not a response itself.
- **`format` line composition** — Made moot by *Terminal stages at non-handler levels*: `format` is handler-only, so there is no cross-level composition to define. Multiple `format` lines at the handler level remain an ordered list (for content negotiation), preserved as the parser produced them.
- **Group nesting via overlap** — When multiple groups match the same handler and they form a containment chain (each pattern is a strict subset of the next), all matching groups contribute to the handler's effective pipeline, layered by specificity. For single-instance stages, the most-specific matching group's declaration wins, but less-specific groups still fill in stages the most-specific group doesn't declare. For multi-instance stages, concatenation order is least-specific groups first, most-specific groups last, all before handler-level steps. Specificity is set inclusion on the route language (group A is more specific than B when every route A matches is also matched by B, but not vice versa). Rationale: matches developer intuition for nested groups (refinement, not replacement); avoids forcing developers to repeat shared concerns at every group level; consistent with the multi-instance composition rule from Q1.
- **Group overlap without containment** — When two matching groups overlap without containment (neither pattern is a strict subset of the other, yet both match the handler), the handler has *ambiguous group membership*. Pipeline elaboration emits a structured error pointing at the conflicting group spans and the affected handler, and the handler's resolved entry inherits only from the system block (skipping every conflicting group) so tooling can still render something. Rationale: override semantics are about which behavior wins, and ambiguous overrides are unsafe — a developer reordering blocks could silently introduce a security or operational bug. Implicit specificity tiebreakers (literals over params, declaration order, etc.) get tangled fast and surprise developers in corner cases. Failing loudly forces an explicit decision. If a real use case ever emerges, an explicit DSL keyword (e.g., `group ... priority N`) is strictly easier to add than to remove a silent precedence rule already in place.
- **`errors` block specificity ranking** — `errors` blocks layer like groups. A handler's *effective error map* is built from every matching `errors` block in specificity order (least-specific first, most-specific last); for each Go error type name, the most-specific matching block's entry wins. Specificity is set inclusion on the route language — the same containment-based definition used for groups. Overlap without containment among matching `errors` blocks produces an *ambiguous errors block membership* elaboration error; the handler's effective error map is built from the remaining matching blocks that form a clean containment chain. Rationale: applying the override model uniformly across groups, multi-instance stages, and `errors` blocks reduces the surface area developers and the runtime must remember; matches the README's "system-level error block provides defaults" language; convention-over-configuration favors one mechanism over per-feature special cases.
- **`none` for terminal stages** — `format none` and `redirect none` are not valid declarations at any level. Pipeline elaboration emits a stage-placement error for them. Rationale: `none` is meaningful only for **composition stages** — those that compose into the pipeline and can be inherited from outer scopes. Terminators (`format`, `redirect`) end the pipeline rather than compose into it; they are handler-only (per Q3) and have no inheritance to clear. A handler with `format none` as its only terminal declaration would have no way to terminate. Composition vs. termination is the organizing principle — `none` applies only to composition stages.
- **`layout none`** — Valid. `layout none` explicitly clears any inherited layout, signaling that the handler renders without a layout wrapper. This is a direct application of the composition framing: layout is a composition stage (it composes the response template into the layout template, is inheritable, and has a runtime default of `templates/layouts/app.html`), so `none` applies. Three distinct states are expressible in the resolved entry via the existing opt-out marker: `layout X` (specific layout), no layout entry (use runtime default), `layout none` (no layout wrapper). Use cases: HTMX partials, embed/widget endpoints, print-friendly views, login pages.
- **Stage interleaving in a level** — Stages must appear in canonical pipeline order in the source. Pipeline elaboration emits a *stage-order violation* error when source order departs from canonical. The canonical sequence for semantic stages is `session` → `csrf` → `limit` → `approve` → `resolve` → `commit` → `emit` → `layout` → `format`/`redirect`; multi-instance stages preserve source order within each kind. **Observational stages** (`log`, `measure`) are exempt — they may appear at any point in source order and are positioned in the effective pipeline at the source positions the developer wrote them, supporting use cases like a `log` between two `resolve` steps for debugging. Rationale: source should reflect execution; silently reordering misleads developers reading the source. Failing loudly catches the mismatch at elaboration time. Observational stages get the carve-out because their position in the pipeline *is* their meaning (a debug log fires where it's written), and they don't change semantic behavior.
- **Resolution against partial ASTs** — Best-effort, mirroring the parser. Pipeline elaboration accepts any AST the parser returns, including partial ASTs from a parser-error pass; partial nodes are skipped silently. Elaboration emits resolved entries for every well-formed handler/group/system block; consumers check `len(parserErrs) == 0` and `len(elaborationErrs) == 0` to determine overall success. Rationale: consistent with the parser's existing best-effort contract from spec 001 (convention over configuration); strict consumers like the runtime and codegen abort on parser errors anyway and won't use the result; tooling consumers (LSP, `writ show`, `writ routes`) want partial elaboration so a single typo does not blank out IDE features for the rest of the file; no double-reporting because the parser already named the failure location and reason.
