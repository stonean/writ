# System

This document captures Writ's cross-cutting architecture — concerns that apply to every feature spec rather than living in any single one. Implementation specs reference this document; this document does not reference implementation specs.

## Configuration

All configuration is read from environment variables at startup. No config files, no YAML, no TOML. Twelve-factor style.

### Source Convention

Source names map to environment variables by convention — source name uppercased plus `_URL`:

| Source name | Environment variable |
| --- | --- |
| `db` | `DATABASE_URL` (special case, industry standard) |
| `cache` | `CACHE_URL` |
| `search` | `SEARCH_URL` |

### Reserved Variables

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `PORT` | no | `8080` | HTTP listen port |
| `WRIT_ENV` | no | `production` | Runtime mode: `development`, `test`, `production` |
| `DATABASE_URL` | when `db` source registered | — | Default database connection string |

### Custom Variables

`w.Config(name, env_var)` exposes additional environment variables to resolvers and commits via context. Every `w.Config` registration is required to resolve at startup; missing variables fail fast with a clear error.

### Local Development

A `.env` file in the project root is loaded automatically when `WRIT_ENV=development`. The `.env.example` file documents every variable the application expects.

## Application Lifecycle

### Startup Sequence

1. Load environment (including `.env` in development).
2. Construct the `writ.Writ` instance and register sources, sessions, emitters, custom resolvers, commits, approvers, formatters, limiters, error handlers, and event listeners.
3. Read `app.writ` (and any included files), parse the DSL, flatten includes.
4. Run startup validation (see below). Any failure aborts before the listener binds.
5. Bind the HTTP listener.

### Startup Validation

Every wiring concern is checked before the first request is served. The validation set is enumerated in the README's *Startup Validation* section. Categories:

- **Reference completeness** — every name used in the DSL (resolvers, commits, approvers, formatters, limiters, error handlers, layouts, templates, body/query types, error types, event listeners, included files) resolves to a registered Go symbol, generated SQL operation, or on-disk asset.
- **Shape consistency** — route parameters, field references (`:model.attribute`), SQL parameter and column names, and body/query struct fields all line up with what they reference.
- **Topology** — resolve and commit dependencies form a valid DAG; no circular references.
- **Pipeline shape** — every handler ends in `format`, `redirect`, or multiple `format` lines for content negotiation; `format` and `redirect` are not mixed except for content negotiation; `using layout` only appears with `.html` formats.
- **Configuration** — every required environment variable resolves; `csrf auto` is paired with a configured session.

### Code Generation Stage

`writ generate` runs as a build step before `go build`. It reads `.writ` and `.sql` files and writes typed Go glue code: route registration, typed field accessors for `:model.attribute` references, SQL parameter binding, result scanning, and compile-time verification of every referenced name. Generated files are committed to the repository — they are readable, debuggable, and diff-able.

## Request Lifecycle

Every request flows through the pipeline stages in order:

1. **log** — record that this happened
2. **measure** — instrument it
3. **session** — load session data from cookie or store
4. **csrf** — validate CSRF token on mutating HTML requests
5. **limit** — gate by rate, short-circuits with 429
6. **approve** — gate by permission, short-circuits with 401/403
7. **resolve** — read data, one or more steps, may depend on each other (independent steps run in parallel)
8. **commit** — write data (multiple commits on the same source share a transaction)
9. **emit** — fire background events (runs after the response is sent, non-blocking)
10. **format** — shape the response, or **redirect** as alternative
11. **log** — record the response

The DSL's `system` block defines pipeline defaults, `group` blocks override them for a route prefix, and handler blocks override the rest. The `none` keyword opts out of an inherited stage.

## Middleware Boundary

Writ owns the request lifecycle as defined by the pipeline stages. Cross-cutting concerns that are not request-specific live outside Writ as standard Go middleware that wraps `w.Handler()`.

**Outside Writ (standard Go middleware):**

- CORS
- Request ID generation
- Compression (gzip)
- Panic recovery
- TLS / HTTPS redirect

**Inside Writ (pipeline stages):**

- Logging
- Metrics
- Session management
- CSRF protection
- Rate limiting
- Authorization
- Input parsing and validation
- Data resolution and mutation
- Background events
- Response formatting
- Error handling

The boundary is: if it cares about business logic or data flow, it is a Writ stage. If it is pure infrastructure, it is standard middleware.

## Workers

`writ.NewWorker()` constructs a stripped-down Writ instance with no HTTP listener and no routes — only event listeners. Workers share the same emitter configuration, the same event handler signatures, and the same registration API as the web process. The same code that runs as `w.On(...)` in-process can be moved into a worker without modification. Worker processes use the `writ worker` CLI verb.

## Shared Infrastructure

| Concern | Owner |
| --- | --- |
| DSL parser, AST, override resolution | DSL/runtime spec |
| Code generation (`writ generate`) | Code generation spec |
| HTTP transport, routing, listener | DSL/runtime spec |
| Source adapters (Postgres, Redis, etc.) | Data layer spec |
| Template loading and rendering | HTML rendering spec |
| Session storage adapters | Sessions spec |
| Event emitters and worker runtime | Background events spec |
| Migration runner | Migrations spec |
| Test runner | Testing spec |

Each owner is a feature spec under `specs/NNN-{slug}/`. Until a spec exists, the area lives in [`inbox.md`](inbox.md).
