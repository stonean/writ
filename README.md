# Writ

Writ is a Go library that provides a declarative DSL for defining HTTP request pipelines. The DSL handles wiring and orchestration. All implementations are written in Go as functions matching defined signatures.

Writ is not a framework or a new language. Developers always have the ability to fall back to custom Go code.

**File extension:** `.writ`

## Core Philosophy

- The DSL describes **what** happens, Go code defines **how**
- Sensible defaults at system level, opt-in overrides per group or handler
- More specific declarations win: handler beats group, group beats system
- `none` keyword explicitly opts out of an inherited default
- The pipeline never reaches into result types — resolvers own their type knowledge
- No reflection — type assertions happen inside resolver functions

## Pipeline Stages

Every request flows through these stages in order:

1. **limit** — rate limiting, short-circuits with 429
2. **log** — request logging
3. **approve** — authorization gate, short-circuits with 401/403
4. **resolve** — data fetching, one or more steps, may depend on each other
5. **format** — shapes the resolved data into the response
6. **error** — handles failures from any stage
7. **metric** — timing and status tracking
8. **log** — response logging

## DSL Syntax

### File Structure

- Encouraged to start as a single file
- `include` for organizational splitting, not a module system
- Runtime flattens all includes as if they were one file
- System block only lives in the root file

```
# app.writ

system ->
  log request, response
  metric timing, status
  limit rate.ip(60/min)
  approve auth.authenticated
  error default error.json

include admin.writ
include users.writ
include public.writ
```

### System Block

Defines defaults inherited by all handlers. Any step defined here applies everywhere unless overridden.

```
system ->
  log request, response
  metric timing, status
  limit rate.ip(60/min)
  approve auth.authenticated
  error default error.json
```

### Group Block

Overrides system defaults for a set of routes. Handlers within the group inherit group settings.

```
group /admin/* ->
  approve auth.isAdmin
  limit rate.ip(10/min)
```

### Handler Block

Defines a single endpoint. Only declares what's unique to it. Inherits from group and system.

```
GET /users/:id ->
  approve auth.isOwner(id) OR auth.isAdmin
  resolve user = db.users(id)
  resolve posts = db.posts(id, limit=10)
  format user,posts user.show.json
```

### Approve

Authorization gate. Supports `OR`, `AND`, `NOT` composition. Implementations are Go functions.

```
approve auth.isOwner(id) OR auth.isAdmin
approve auth.authenticated
approve none
```

### Resolve

Data fetching. Each resolve stores its result by name. Results can be passed as arguments to subsequent resolves.

```
resolve user = db.users(id)
resolve posts = db.posts(id, limit=10)
resolve teammates = db.team_members(user)
```

Three argument types:
- **Route parameters**: `id` — extracted from the route definition
- **Previous resolve results**: `user` — the whole result object, passed as-is
- **Static values**: `limit=10` — named value defined in the DSL

The reserved keyword `body` provides access to the request body as an `io.ReadCloser`:

```
POST /users ->
  resolve user = parse_user(body)
  resolve resp = db.users.create(user)
  format resp user.json
```

The pipeline never accesses fields on results. When a resolve needs data from a previous result, the entire result object is passed and the resolver function handles field access via type assertion.

Independent resolves with no data dependencies between them are executed in parallel automatically.

### Format

Two parts: the data to format, and the registered formatter name.

```
format user user.show.html
format user,posts user.show.json
format status health.json
```

Multiple data sources are comma-separated. The formatter name is the registration key — it can encode any convention the developer wants (e.g., `user.show.html`, `user.edit.json`).

### Error

Two parts: the error handler name, and the formatter name for error output.

```
error default error.json
error profile error.html
```

The error handler produces data. The formatter shapes it. They are independent of the handler's format step.

### Override Rules

For any given step type, the most specific declaration wins:
- Handler-level overrides group-level
- Group-level overrides system-level
- `none` explicitly removes an inherited step

## Open Questions

- Resolve chaining and the `any` type — runtime panics vs compile-time safety
- Middleware (CORS, request IDs, compression) — Writ's boundary vs standard Go middleware
- Source interface shape — CRUD-focused vs more flexible
- Response status code control (200 vs 201 vs 204)
- Testing patterns for handlers, resolvers, and full pipelines
- Configuration management (env vars, secrets, connection strings)
