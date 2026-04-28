# 005 — Code Generation Plan

## Overview

The generator is a new package, `codegen`, plus a new CLI binary, `cmd/writ`. The package consumes the pipeline's `*pipeline.Resolved` IR (already produced by spec 002 from spec 001's AST) and emits Go source files that call into the spec 003/004 runtime. Discovery of the DSL→Go identifier mapping is handled by a new set of exported wire types in the existing `writ` package, parsed at codegen time from consumer source via `go/packages` + `go/ast`.

The runtime is not modified except to add four exported map-shaped types (`WireResolvers`, `WireFormatters`, `WireErrorFormatters`, `WireErrorTypes`) whose declarations have no runtime effect — the runtime never reads them. All semantic work happens in the generator.

## Technical Decisions

### Package layout

| Package | Purpose |
| --- | --- |
| `github.com/stonean/writ/codegen` | Generator library: walks `Resolved`, walks wire declarations, emits Go source. Pure-logic, no `os` writes. |
| `github.com/stonean/writ/codegen/wire` | Wire-declaration discovery: load consumer package via `go/packages`, extract `var _ = writ.WireXxx{...}` composite literals into a name→identifier map. |
| `github.com/stonean/writ/codegen/scaffold` | Scaffolding mode: stamp out `<stem>.go` from a single `.writ` source. Pure-logic, returns `[]byte`. |
| `github.com/stonean/writ/cmd/writ` | New CLI binary. Subcommand router; the `generate` and `generate scaffold` verbs delegate to the packages above. Owns all filesystem I/O. |

The split keeps the IO boundary at `cmd/writ`. Library packages return bytes; the CLI writes them. This matches the constitution's testability principle and lets unit tests exercise generation without touching disk.

The wire types live in package `writ` alongside the runtime types they wire (per the spec's Resolved Question on "Bootstrap order"). They are added in the wire-types task; no other runtime change is required.

### Wire types in `writ`

Add to the existing `writ` package:

```go
// WireResolvers maps DSL resolver names to Go function values for the
// generator to read at codegen time. Declarations have no runtime effect.
type WireResolvers map[string]any

// WireFormatters maps DSL formatter names to Go function values.
type WireFormatters map[string]any

// WireErrorFormatters maps DSL error-formatter names to Go function values.
type WireErrorFormatters map[string]any

// WireErrorTypes maps DSL error-type names to zero-value error instances.
type WireErrorTypes map[string]any
```

`map[string]any` (rather than typed function maps like `map[string]ResolverFunc`) is the deliberate choice. The right-hand side of each entry is a typed Go expression; the Go compiler enforces existence of every named identifier through ordinary identifier resolution. The generator only needs the mapping (DSL string key → Go identifier name), which the AST provides without needing the value's type. Any tighter typing would force consumers to wrap their domain functions in adapter literals at the wire site, which defeats the wire block's role as a thin index.

Consumers assign via `var _ = writ.WireResolvers{...}` so the map literal is constructed-then-discarded; the generator parses the AST of these declarations at codegen time and never executes them.

### CLI surface (`cmd/writ`)

| Verb | Flags | Behavior |
| --- | --- | --- |
| `writ version` | — | Prints the binary's version (matches the version embedded in generated file headers). |
| `writ generate` | `[--dry-run] [--check]` | Walks the current directory's package, generates `writ_gen.go`, writes it. `--dry-run`: print would-be files, do not write. `--check`: compute in memory, diff against on-disk, exit non-zero with unified diff on mismatch, no writes. |
| `writ generate scaffold <writ-file>` | `[--force]` | Stamps `<stem>.go` next to `<writ-file>`. Refuses to overwrite without `--force`. Always exits non-zero if the destination exists and `--force` is absent. |

Subcommand dispatch uses Go's `flag` package on a manual `os.Args` walk — sufficient for three verbs and two flag groups, no third-party CLI library introduced.

The version string is a `const Version = "0.0.0"` baked into `cmd/writ` at build time — initial value placeholder until release tooling sets it. The spec's `--check` mode comparing the version line is satisfied because both the in-memory and on-disk files use the same `Version` constant within a single binary build.

### Generator IR

The generator's internal IR is a slice of `codegen.HandlerPlan`, derived from `*pipeline.Resolved.Handlers` plus the wire map. Each `HandlerPlan` carries:

- `Method` — the HTTP method (already canonical-case from parser/elaboration).
- `Pattern` — `*ast.RoutePattern` (segments, including `*ast.ParameterSegment` entries).
- `Identifier` — the generated Go identifier root (`{Method}{PascalCasePath}`), computed by `codegen.MakeIdentifier(method, pattern)`.
- `RequestStructName` — `Identifier + "Request"`.
- `RegisterFuncName` — `"Register" + Identifier`.
- `ResolveCalls` — list of `ResolveCallPlan{Name, Args, GoFuncIdent}`. `GoFuncIdent` is looked up from the wire map; an entry missing from the wire map is a generator diagnostic.
- `Format` — the terminal `FormatStage` (or `RedirectStage`) with `GoFuncIdent` for the formatter.
- `ErrorMap` — list of `ErrorArmPlan{TypeName, FormatterName, IsDefault, GoTypeIdent, GoFormatterIdent}`. Order is spec-002's `Handler.ErrorMap` order, preserved verbatim.

Identifier generation follows the spec's resolved naming convention (`{Method}{PascalCasePath}` with `By{PascalCase}` for parameter segments, role suffixes, fixed acronym list). Implemented as a pure function over `(method string, pattern *ast.RoutePattern)`. Collisions within a single package produce a generator error (`KindIdentifierCollision`) before any output is written.

### Wire-declaration parsing

The wire package uses `go/packages` with mode `NeedTypes | NeedSyntax | NeedTypesInfo` to load the package containing the `.writ` files. It walks each file's top-level `*ast.GenDecl` looking for `var _ = <selector>{...}` patterns where `<selector>` resolves (via `pkg.TypesInfo`) to one of the four wire types in the `writ` package — alias-tolerant because the resolution is type-driven, not name-driven.

Each entry in the composite literal is `*ast.KeyValueExpr` with:

- `Key`: a `*ast.BasicLit` of kind `STRING` (the DSL name).
- `Value`: an `*ast.Ident` or `*ast.SelectorExpr` (the Go identifier).

The generator records the (DSL name → fully-qualified Go identifier) mapping. The Go compiler — invoked transitively by `go/packages` via type checking — enforces that every right-hand side identifier exists; an undefined identifier surfaces as a `pkg.Errors` entry, which the generator forwards to its own diagnostic stream (re-framed with the wire entry's source position).

For `WireErrorTypes` specifically, the Value expression is a zero-value of an error type (e.g., `NotFound{}`). The generator uses `pkg.TypesInfo.TypeOf(value)` to recover the Go type; for the `errors.As` chain it emits, the type identifier is enough.

A wire entry whose key is a duplicate DSL name within the same wire-type map produces a generator diagnostic (`KindWireDuplicateKey`) and the generation aborts with no output.

### Generated `writ_gen.go` contents

Per package, one `writ_gen.go` containing:

1. The two-line header (per the spec's Versioning resolved question):

   ```go
   // Code generated by writ generate v0.0.0. DO NOT EDIT.
   // Source: handler1.writ, handler2.writ
   ```

   The `Source:` list is lexically sorted, comma-separated.

2. `package <name>` matching the consumer package.

3. Imports — minimally `context`, `errors`, `net/http`, and the framework `writ` package. The set is computed from the handlers actually emitted; unused imports are not written.

4. For each `HandlerPlan` in lexically-sorted order by `Identifier`:

   - The per-handler request struct: one `string` field per `ParameterSegment.Name` (default-typed `string` per the resolved question on parameter types).
   - The `Register{Name}` function — sole entry point the consumer's `main()` calls. It accepts typed function values (one per resolve step plus the formatter and one per error formatter), wraps them in closure adapters that bridge to `writ.ResolverFunc` / `writ.FormatterFunc` / `writ.ErrorFormatterFunc`, and calls `(*Writ).Resolver` / `Formatter` / `ErrorFormatter` plus `writ.ErrorType[T]` as appropriate.
   - The closure adapters extract route parameters from `Params` into the request struct, invoke the consumer-supplied typed function, and store the result back into the runtime's named-result table via the existing `Results.Get`/`Has` accessors. They invoke registered error matchers via `errors.As(err, &target)` per error-map entry, in spec-002's most-specific-first order — preserving wrapped-error matching from spec 004.

5. No timestamp, no random identifier, no toolchain-version stamp. The version line is the only thing that varies across CLI versions.

The output is post-processed through `go/format.Source` for canonical Go formatting. Consumers regenerate after Go toolchain upgrades per the resolved Q8.

### Typed dispatch (no `reflect`)

The generated dispatch loop emits a sequence of `if errors.As(err, &t<i>)` arms — not a Go `switch err := err.(type)` block. The reason is wrapped-error semantics: the spec-004 runtime walks the `Unwrap` chain via `errors.As`; a naked type switch only matches direct types and would silently break wrapped-error dispatch. The acceptance criterion "Wrapped errors match the same registered type they would match under the spec-004 runtime path" forces the `errors.As` chain.

The generated file imports `errors` (stdlib) but not `reflect`. `errors.As` itself uses reflection inside the stdlib, which the spec explicitly accepts ("the generator does not use reflect" applies to the generator's emitted file, not to stdlib internals).

Order: each `ErrorMapEntry` produces one arm, in the order spec-002 already produced (most-specific-first; concrete types before `default`). The first match wins; subsequent arms are not evaluated. The `default` entry (if present) becomes the trailing fallthrough that does not call `errors.As`.

### Scaffolding (`writ generate scaffold`)

Generates `<stem>.go` from a single `.writ` file. The file is consumer-owned from the moment of creation — no `// Code generated ... DO NOT EDIT.` header. Contents:

- `package <name>` (inferred from the directory, defaulting to the directory's basename if no Go files exist yet).
- Stub functions for each resolver, formatter, error formatter, and error type referenced in the `.writ` source. Each stub has a body containing a `// TODO: implement` comment and a return statement that satisfies its declared signature. The signature is derived from the runtime's declared shapes (`writ.ResolverFunc`, `writ.FormatterFunc`, `writ.ErrorFormatterFunc`) — generic until typed inputs/outputs are introduced by future specs.
- Domain-type stubs for any error type referenced (e.g., `type DuplicateEmail struct{}` with a `StatusCode() int` method returning 500 by default).
- A populated wire block that maps every DSL name to the stub identifier.

Refuses to overwrite an existing `<stem>.go`. The CLI verifies file existence before invoking `scaffold.Generate`; library code is overwrite-agnostic.

### Diagnostics

The generator collects diagnostics from four sources and prints them in `file:line:col: kind: message` format (matching parser/pipeline conventions):

- **Parse errors** — surfaced verbatim from `parser.Parse`.
- **Elaboration errors** — surfaced verbatim from `pipeline.Elaborate`.
- **Wire-side mismatch** — `KindWireMissingEntry`. A `.writ` file references a DSL name (e.g., a resolver) for which no wire entry exists. Diagnostic points at the `.writ` source span; message includes which wire-block the entry should live in.
- **Wire-side stale** — `KindWireStaleIdentifier`. A wire entry references a Go identifier that does not exist in the package. Surfaced from `go/packages` `pkg.Errors` and re-framed with the wire entry's source location.
- **Identifier collision** — `KindIdentifierCollision`. Two handlers reduce to the same Go identifier. Diagnostic points at both `.writ` source spans.

All four kinds participate in atomic output: any non-empty diagnostic set means no file is written and the CLI exits 1. `--dry-run` honors the same atomicity (diagnostics still print; no would-be filesystem report on failure). `--check` exits 1 on diagnostics regardless of whether on-disk file content matches.

Diagnostics are collected, not short-circuited — multiple errors print together, matching spec 001/002 behavior.

### Determinism

- Handlers are sorted by `(Method, PatternString)` ASCII order before emission.
- Imports are sorted by `go/format.Source`.
- Source list in the header is lexically sorted.
- Wire-map iteration uses sorted keys, never raw map iteration (Go map iteration is non-deterministic; codegen requires deterministic walks).
- Within each handler, error-map arms preserve `pipeline.Handler.ErrorMap` ordering (spec 002 already deterministic).

Identifiers are deterministic functions of source identifiers — no counters, no source-position-derived suffixes. Renaming a `.writ` source file does not change any generated identifier.

### Integration with `go generate`

Consumers add `//go:generate writ generate` to a Go source file in each package containing `.writ` files. `go generate ./...` invokes the binary the same way the user invokes it directly. The generator does not depend on any `go generate`-specific environment variables (`$GOFILE`, `$GOPACKAGE`, etc.) — the directive's working directory provides the package path, and `go/packages` discovers the rest.

### Versioning constant

A single `Version = "0.0.0"` constant lives in `cmd/writ` and is referenced by the codegen package via a function parameter (rather than a global). This keeps `codegen` IO-free and makes the version injectable for testing without environment manipulation.

## Affected Files

| File | Action | Purpose |
| --- | --- | --- |
| `writ/wire.go` | Create | Define `WireResolvers`, `WireFormatters`, `WireErrorFormatters`, `WireErrorTypes` types in the runtime package. |
| `writ/wire_test.go` | Create | Verify the types are `map[string]any`-shaped and have no runtime effect. |
| `codegen/doc.go` | Create | Package doc comment. |
| `codegen/identifier.go` | Create | `MakeIdentifier(method string, pattern *ast.RoutePattern) string` plus the fixed acronym list. |
| `codegen/identifier_test.go` | Create | Table-driven tests covering the resolved naming convention's examples and edge cases. |
| `codegen/plan.go` | Create | `BuildPlan(*pipeline.Resolved, wire.Map) (*Plan, []Diagnostic)` and the IR types (`HandlerPlan`, `ResolveCallPlan`, `ErrorArmPlan`). |
| `codegen/plan_test.go` | Create | Tests for IR construction and collision detection. |
| `codegen/emit.go` | Create | `Emit(*Plan, version string) ([]byte, error)`; wraps `text/template` + `go/format.Source`. |
| `codegen/emit_test.go` | Create | Golden-file tests verifying byte-stable output. |
| `codegen/diagnostic.go` | Create | Diagnostic kinds (`KindWireMissingEntry`, `KindWireStaleIdentifier`, `KindIdentifierCollision`, `KindWireDuplicateKey`) and formatting. |
| `codegen/check.go` | Create | `Check(generated, onDisk []byte) (diff string, ok bool)` — unified diff for `--check`. |
| `codegen/wire/discover.go` | Create | `Discover(pkgPath string) (Map, []Diagnostic)` — `go/packages`-based wire-declaration parser. |
| `codegen/wire/discover_test.go` | Create | Tests with synthetic packages exercising every wire type. |
| `codegen/scaffold/scaffold.go` | Create | `Generate(prog *ast.Program, packageName string) ([]byte, error)`. |
| `codegen/scaffold/scaffold_test.go` | Create | Golden-file tests for scaffolded output. |
| `codegen/testdata/` | Create | `.writ` inputs and golden Go outputs for emit and scaffold tests. |
| `cmd/writ/main.go` | Create | CLI entry point; subcommand dispatch. |
| `cmd/writ/version.go` | Create | `const Version = "0.0.0"`. |
| `cmd/writ/generate.go` | Create | `writ generate` and `writ generate scaffold` handlers. |
| `cmd/writ/generate_test.go` | Create | End-to-end tests using `t.TempDir()`. |
| `go.mod` | Modify | Add `golang.org/x/tools/go/packages` dependency (required for wire discovery). |
| `README.md` | Modify | Document the `writ generate` workflow and the wire block convention. |

## Data Model

The feature introduces three categories of structures:

- The four exported wire-map types in the `writ` package (consumer-facing).
- The generator's internal IR (`Plan`, `HandlerPlan`, `ResolveCallPlan`, `ErrorArmPlan`).
- The wire-discovery output (`wire.Map`).

These are detailed in [data-model.md](data-model.md).

## Trade-offs

### `go/packages` as a dependency

`golang.org/x/tools/go/packages` adds a non-stdlib dependency. Considered alternatives:

- **`go/parser` + manual import resolution.** Rejected — type information is required to identify wire-type composite literals through aliases, and reimplementing the type checker is out of proportion for this feature.
- **`go list -json`.** Provides import metadata but not full type info; would force a second pass for type checking.

`go/packages` is the canonical Go-ecosystem choice for build-time AST + type analysis (used by `staticcheck`, `errcheck`, `golangci-lint`, etc.) and is already transitively present in the developer toolchain.

### Per-handler request structs

Spec resolution selects per-handler structs over a shared shape. The plan accepts the resulting N-structs-per-package size cost as documented. Generated identifiers are discoverable via `go doc`, satisfying the discoverability concern.

### `errors.As` chain vs. type switch

Type switches are faster (no reflection per arm) but break wrapped-error matching. The spec mandates wrapped-error parity with spec 004; `errors.As` chain is the only mechanism that satisfies this. The performance differential is negligible — error paths are not hot — and the alternative (a spec change to allow wrapped-error semantic divergence) is out of scope.

### CLI version constant

A baked-in `Version = "0.0.0"` placeholder shifts versioning into release tooling rather than build-time injection via `-ldflags`. This keeps the developer flow simple (`go build ./cmd/writ` produces a working binary). Release tooling can later inject via `-ldflags` without code changes — variable, not constant — when the project introduces a release pipeline.

### Atomic-on-error writes

The generator writes through `os.WriteFile` to a temp file, then `os.Rename` to the final path, so a partially-written file never replaces a working `writ_gen.go`. Considered: writing in-place. Rejected because `os.Rename` provides POSIX atomicity at no cost and protects against process-kill scenarios.

### No runtime fallback removal

Per the resolved question, the runtime path is not deprecated, gated, or marked. Generated code coexists with hand-rolled registration uniformly; the runtime treats both kinds of handlers identically.

## Open Questions Resolved

All open questions are resolved in `spec.md`. The plan's mappings:

- **Runtime fallback removal path** — Plan introduces no deprecation hook; the existing runtime API is unchanged.
- **Versioning of generated code** — Plan emits the two-line header with `Version` constant; `--check` compares the full file including the version line.
- **Bootstrap order** — Plan places wire types in package `writ` (not a sub-package), and the generator binary depends on `parser`/`pipeline`/`ast` plus `go/packages`, never on the runtime package.
- **CI hygiene** — Plan implements `writ generate --check` as a `Check([]byte, []byte) (diff, ok)` library function plus a CLI flag.
- **Validation interaction** — Plan emits registration calls that satisfy the existing `KindUnregisteredResolver`/`Formatter`/`ErrorType`/`ErrorFormatter` checks; nothing is bypassed.
- **Toolchain-version stability** — Plan uses `go/format.Source` (stdlib formatter) and accepts cosmetic shifts across Go versions; semantic content is stable.
- **Identifier naming convention** — `MakeIdentifier` implements the resolved rule, including the fixed acronym list (`ID`, `URL`, `API`, `HTTP`, `JSON`, `XML`, `UUID`, `IP`, `TCP`, `UDP`).
- **Behavior on invalid input** — Plan defines four diagnostic kinds plus passthrough of parse/elaborate diagnostics; atomic write semantics enforced via temp+rename.
- **Default `:name` segment type** — `string`, with no escape hatch.
- **Per-handler request struct** — Plan emits one `<Identifier>Request` struct per handler.
- **Wire discovery mechanism** — Plan implements wire-declaration parsing in `codegen/wire`.
- **Generated file layout** — One `writ_gen.go` per package, written next to the `.writ` files.
