---
name: Inbox
description: Backlog of feature areas described in the README that have not yet been written up as feature specs. Each item becomes a numbered spec under `specs/NNN-{slug}/` when work begins, via `/specify` or `/capture`.
---

# Inbox

Temporary inbox for known feature areas not yet assigned to a feature spec. Items are migrated to their proper home as specs are written.

<!-- Rules:
     - Do not frontfill bugs that are not being actively worked on.
     - Write specs for areas being actively touched — let adoption spread naturally.
     - As specs are written, items migrate from here into spec updates or new scenarios.
     - The goal is for this file to eventually be empty and deleted. -->

## Feature Areas

The README defines Writ's full surface. Each item below is a slice of that surface that needs its own spec before implementation. They are listed roughly in the order a runtime would need them, not as a fixed build order.

- **DSL parser** — lexer, grammar, AST, include flattening. Produces a fully resolved AST from one or more `.writ` files; does not execute anything.
- **Pipeline runtime** — given a parsed AST, executes the request pipeline: stage ordering, override resolution (handler beats group beats system), parallel resolve scheduling, transaction wrapping for commits, content negotiation. Depends on the parser.
- **Code generation (`writ generate`)** — read `.writ` + `.sql` files, produce typed route registration, typed field accessors for `:model.attribute` references, parameter binding and result scanning glue.
- **Startup validation** — every check enumerated in the README's *Startup Validation* section, run before the server accepts requests.
- **Data layer** — SQL files in `queries/` with `-- name:` headers, `$param` binding by lowercased struct field name, type scanning by capitalized resolve name, custom Go resolver override, automatic transaction wrapping for multiple commits.
- **Typed input structs** — JSON body parsing (`json` tags), multipart form parsing (`form` tags), `writ.File` for uploads, query parameter parsing (`query` tags), defaults, and `validate` tag enforcement.
- **Sessions** — `session cookie` declaration, pluggable storage (cookie / Redis / database), session API exposed through context, login/logout commit pattern.
- **CSRF protection** — `csrf auto`, automatic token issuance per session, validation on mutating HTML routes, `{{ .CSRFToken }}` and `{{ .CSRFField }}` template helpers, JSON route exemption.
- **HTML rendering** — template-name-to-filesystem convention, layouts with `templates/layouts/`, layout inheritance via the override model, `using layout` keyword, auto-generated formatters from disk, static asset serving from `public/`.
- **Background events (`emit`)** — local goroutine emitter, NATS and NATS JetStream emitters, `writ.NewWorker()` worker process, `writ.Queue(...)` queue groups, multiple listeners per event.
- **Errors** — `errors /pattern ->` block, type-matched dispatch, `default` catch-all, `StatusCode()` method on error types, route-pattern override model.
- **Migrations** — timestamped SQL files in `migrations/`, up/down sections, `writ_migrations` tracking table, `writ migrate` CLI verbs (new / up / down / status), test-suite integration.
- **Configuration** — env-var convention (source name → `*_URL`), `PORT`, `WRIT_ENV`, `w.Config(name, env_var)` for custom values, `.env` loading in development, fail-fast on missing required vars.
- **Testing DSL (`.test.writ`)** — `users` and `fixtures` blocks, request line format (`as <user> METHOD path with <fixture>`), `expect` assertions, `capture`, `seed`, fresh-database execution model, `writ test` runner.
- **Development mode (`writ dev`)** — filesystem watcher, full restart on `.writ` / `.sql` / `.go` changes, hot reload (no restart) on `.html` template changes.
- **CLI tooling** — `writ generate`, `writ run`, `writ dev`, `writ worker`, `writ test`, `writ show <route>`, `writ routes`, `writ migrate {new,up,down,status}`.
- **Code formatter (`writ fmt`)** — canonical AST→text rendering, idempotent (running twice produces the same output), preserves comments and includes. Depends on the parser AST. Modeled on `gofmt`: language stays permissive, formatter is opinionated.

## Open Items

- **AI resolver source (Kronk integration)** — out-of-tree **adapter package** (e.g., `github.com/stonean/writ-kronk`) that exposes [ardanlabs/kronk](https://github.com/ardanlabs/kronk) (embedded llama.cpp via `hybridgroup/yzma`) as a `Source` plus named `ai.*` resolvers/commits (`ai.complete`, `ai.embed`, `ai.rerank`, `ai.classify`). Same pattern as in-tree adapters (`writ.Postgres()`, `writ.NATS()`) — Writ exposes interfaces, the adapter package implements them, the user registers via `w.Source("ai", kronk.Source(...))`. Lives in its own module because Kronk pulls in cgo, Badger, OPA, OTel, and the MCP SDK, which conflicts with "start small, stay small." Pairs naturally with SQL-as-resolver for pgvector-backed semantic search. Spec only when a real consumer surfaces. Note: Writ does not have (and does not need) a formal plugin system — adapter packages are just regular Go modules consumed via standard imports.
- **Log levels (debug, info, warn, error)** — `log` argument syntax for level annotation (e.g., `log request:info`, `log debug "user resolved" with user`), or convention that level is determined by the logger registration outside the DSL. Affects DSL grammar (parser surface) and runtime logger registration. Currently `log` accepts identifiers as args (`log request, response`); the README does not address levels. Surface when a real consumer needs to differentiate log severity. Came up while resolving spec 002 Q10.

## Brownfield Security Audit Findings

- [ ] BE-INPUT-001: specs/003-runtime-skeleton/spec.md does not address — accepts URL path parameters as client input but names no server-side schema validation mechanism
- [ ] BE-INPUT-001: specs/003-runtime-skeleton/plan.md does not address — extracts route parameters into Params accessor with no validation step before passing to resolvers
- [ ] BE-DATA-001: specs/003-runtime-skeleton/spec.md does not address — binds an HTTP listener with no commitment to TLS 1.2+ or rejection of plaintext transport
- [ ] BE-DATA-001: specs/003-runtime-skeleton/plan.md does not address — Run constructs http.ListenAndServe over plain HTTP with no TLS configuration named
- [ ] BE-API-001: specs/003-runtime-skeleton/spec.md does not address — describes HTTP responses but commits to no security headers (HSTS, X-Content-Type-Options, Referrer-Policy, CSP, Cache-Control)
- [ ] BE-API-001: specs/003-runtime-skeleton/plan.md does not address — runtime explicitly does not touch response headers on success path and sets none for the runtime-owned 500 beyond Content-Type
- [ ] BE-API-001: specs/004-errors-block/spec.md does not address — error response path writes Content-Type only, omits HSTS, X-Content-Type-Options, Referrer-Policy, Cache-Control commitments
- [ ] BE-API-001: specs/004-errors-block/plan.md does not address — writeStatusText and write500 set only Content-Type, no other security headers committed for error responses
- [ ] BE-API-004: specs/003-runtime-skeleton/spec.md does not address — introduces public HTTP endpoints with no rate-limit policy named (limit stage explicitly out of scope)
- [ ] BE-API-004: specs/003-runtime-skeleton/plan.md does not address — routing table dispatches public requests with no per-IP, per-user, or per-token throttling mechanism
- [ ] BE-ERR-002: specs/003-runtime-skeleton/spec.md does not address — error responses are plain-text strings without stable error code, structured format, or correlation ID
- [ ] BE-ERR-002: specs/003-runtime-skeleton/plan.md does not address — write500 emits a raw "500 Internal Server Error\n" body with no code field or request correlation ID
- [ ] BE-ERR-002: specs/004-errors-block/spec.md does not address — fallback path writes "<status> <reason>\n" plain text and the error formatter contract names no code/correlation-ID requirement
- [ ] BE-ERR-002: specs/004-errors-block/plan.md does not address — writeStatusText emits unstructured plain text with no RFC 7807 commitment or correlation-ID field
- [ ] BE-ERR-003: specs/003-runtime-skeleton/spec.md does not address — explicitly defers panic recovery to net/http defaults rather than committing to a global structured exception handler
- [ ] BE-ERR-003: specs/003-runtime-skeleton/plan.md does not address — ServeHTTP installs no recover() and relies on the standard library's default panic-to-500 behavior
- [ ] BE-ERR-003: specs/004-errors-block/spec.md does not address — error path handles only resolver errors and inherits spec 003's no-panic-recovery stance with no structured 5xx commitment
- [ ] BE-ERR-003: specs/004-errors-block/plan.md does not address — handleResolverError covers only resolver-returned errors; no global handler wraps the dispatch path against unhandled exceptions
- [ ] FE-CSRF-001: specs/003-runtime-skeleton/spec.md does not address — accepts arbitrary HTTP methods including state-changing POST/PUT/PATCH/DELETE with no CSRF defense (csrf stage explicitly out of scope)
- [ ] FE-CSRF-001: specs/003-runtime-skeleton/plan.md does not address — routing table dispatches state-changing methods to handlers with no synchronizer-token or SameSite-cookie strategy named
