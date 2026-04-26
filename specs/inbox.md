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

- **AI resolver source (Kronk integration)** — optional `Source` adapter that exposes [ardanlabs/kronk](https://github.com/ardanlabs/kronk) (embedded llama.cpp via `hybridgroup/yzma`) as named `ai.*` resolvers/commits (`ai.complete`, `ai.embed`, `ai.rerank`, `ai.classify`). Pairs naturally with SQL-as-resolver for pgvector-backed semantic search. Must be a separate adapter package, not a core dep — Kronk pulls in cgo, Badger, OPA, OTel, and the MCP SDK, which conflicts with "start small, stay small." Spec only when a real consumer surfaces.
