# Project: writ

Opinionated Go web framework with a declarative DSL for HTTP request pipelines

## Constitution

See [constitution.md](constitution.md) — guiding principles, development pipeline, spec lifecycle, and quality standards that govern this project.

## Tech Stack

| Layer | Technology | Role |
| --- | --- | --- |
| **Language** | Go 1.26 | DSL parser, pipeline elaboration, runtime, code generation |

## Commands

Per-package verification suite — run every one of these on the affected Go package(s) before reporting a task done or proposing a commit. None may be skipped; all must be clean (or every remaining warning must be a documented expected-to-clear-imminently case noted in the commit message).

| Tool | Invocation | Purpose |
| --- | --- | --- |
| `gofmt` | `gofmt -l <pkg>/` | Empty output means no files need reformatting |
| `go vet` | `go vet ./<pkg>/...` | Catches suspicious constructs the compiler accepts |
| `staticcheck` | `staticcheck ./<pkg>/...` | Style + correctness checks beyond `go vet` |
| `errcheck` | `errcheck ./<pkg>/...` | Catches unchecked errors |
| `golangci-lint` | `golangci-lint run ./<pkg>/...` | Aggregate linter (defaults are sufficient) |
| `govulncheck` | `govulncheck ./<pkg>/...` | Known-vulnerability scan |
| Build | `go build ./<pkg>/...` | Must succeed |
| Test | `go test ./<pkg>/... -race -shuffle=on -count=1` | Must pass; `-race` catches data races, `-shuffle=on` catches order-dependent tests, `-count=1` defeats the test cache |
| Module tidy | `go mod tidy && git diff --exit-code go.mod go.sum` | `go.mod`/`go.sum` must be clean — non-empty diff means deps are stale |
| Generated drift | `go generate ./<pkg>/... && git diff --exit-code` | Generated output must match sources — non-empty diff means a `//go:generate` directive needs to be re-run |

Tooling notes:

- `staticcheck` and `errcheck` must be built against the module's required Go version (currently 1.26). If a tool refuses to run with a `version mismatch` message, rebuild it: `go install honnef.co/go/tools/cmd/staticcheck@latest`, `go install github.com/kisielk/errcheck@latest`, `go install golang.org/x/vuln/cmd/govulncheck@latest`.
- `staticcheck` and `golangci-lint` may report `unused` warnings during multi-task implementation when an upcoming task wires the constructor or helper. These are acceptable transient warnings — note them in the commit message and confirm they clear when the dependent task lands. Do not suppress with `//nolint`; let them resolve naturally.
- `golangci-lint` re-runs several of the standalone linters above (`gofmt`, `go vet`, `staticcheck`, `errcheck`) depending on its configuration. Running them individually first is intentional: failures surface faster and with clearer per-tool output.
- The module-tidy and generated-drift checks rely on `git diff --exit-code`. Run them from a clean working tree (or stage your intended changes first) so the diff reflects only tool output, not in-progress edits.

## Project Structure

- `constitution.md` -- Principles, pipeline, quality standards
- `AGENTS.md` -- Agent rules: tech stack, conventions, boundaries
- `CLAUDE.md` -- `@AGENTS.md` + Claude Code-specific configuration
- `specs/`
  - `system.md` -- Architecture, shared conventions
  - `events.md` -- Global event catalog
  - `errors.md` -- Error handling conventions and codes
  - `inbox.md` -- Temporary inbox for known issues not yet assigned to a spec
  - `templates/` -- Starter files for specs, plans, tasks, scenarios
  - `{NNN-feature}/`
    - `spec.md` -- Requirements, contracts, acceptance criteria
    - `research.md` -- *(optional)* Background research, prior art, context
    - `plan.md` -- Implementation approach, technical decisions
    - `data-model.md` -- *(optional)* Generated during plan phase
    - `tasks.md` -- Discrete work items derived from the plan
    - `scenarios/` -- Bug fixes, edge cases, detailed behavior elaborations
      - `{slug}.md` -- Individual scenario created via the scenario command

<!-- Add project-specific directories below (e.g., src/, cmd/, modules/) -->

## Code Style

<!-- Define code patterns and conventions specific to your tech stack. -->

## Testing

<!-- Define testing conventions, test types, and file placement. -->

## Gotchas

<!-- Document things agents consistently get wrong — framework quirks, version-specific behavior,
     and non-obvious conventions that waste cycles. Focus on what's surprising, not what's standard.
     Example:

- pgx v5 uses `pgx.RowToStructByName`, not the older `Scan` pattern
- templ components must not import `net/http` — pass values through parameters
- `air` does not watch `.sql` files by default — add the pattern to `.air.toml`
- NATS JetStream consumer names must be globally unique, not just per-stream

-->

## DSL Boundaries

The Writ DSL (`.writ` files) describes request flow only — pipeline stages (`resolve`, `commit`, `approve`, `format`, `error`, `log`, `metric`, `limit`) and the handler, group, and system blocks that compose them. Data shape (models, record types, field constraints) and component wiring (sources, resolvers, formatters, approvers, error handlers) are declared in Go and referenced from the DSL by name.

When designing a new Writ feature, ask: does this describe request flow? If yes, it can live in the DSL. If it describes data shape, validation rules, or component registration, put it in the Go API and have the DSL reference it by name.

## Spec Conventions

- Writ feature specs include **both** DSL reference forms and representative Go registration samples, paired so the binding between a Go registration and its DSL name is visible. Writ's public contract is its Go API plus the DSL; excluding Go from specs makes them ambiguous. This is a project-specific refinement of the constitution's general guidance against language-specific code in specs.
- Go samples in specs are contract illustrations, not finalized signatures — exact spellings of types, fields, and options are refined in the plan phase.

## Boundaries

- Before working on any spec beyond its spec.md, verify all dependency specs have status `done`. If any dependency is not done, work on the earliest incomplete dependency instead.
- Follow tasks.md literally — do not skip ahead to later pipeline phases. When tasks say to set status to `planned`, stop there. The user advances to the next phase explicitly.

<!-- Define additional project-specific boundaries. Common patterns:

- Never implement without a spec — follow the pipeline: spec → plan → tasks → implement
- Never commit secrets or .env files
- Never modify CI/CD config without asking
- Ask before adding new dependencies
- Ask before changing database schema

-->
