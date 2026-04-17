# Writ

Writ is an opinionated Go web framework built around a declarative DSL for defining HTTP request pipelines. The DSL handles wiring and orchestration. All implementations are written in Go as functions matching defined signatures.

Convention over configuration. Start small, stay small.

**File extension:** `.writ`

## Core Philosophy

- The DSL describes **what** happens, Go code defines **how**
- Convention over configuration — predictable structure, naming, and defaults
- Sensible defaults at system level, opt-in overrides per group or handler
- More specific declarations win: handler beats group, group beats system
- `none` keyword explicitly opts out of an inherited default
- Code generation over reflection — typed resolver chains validated at compile time

## Pipeline Stages

Every request flows through these stages in order:

1. **log** — record that this happened
2. **measure** — instrument it
3. **limit** — gate by rate, short-circuits with 429
4. **approve** — gate by permission, short-circuits with 401/403
5. **resolve** — read data, one or more steps, may depend on each other
6. **commit** — write data
7. **format** — shape the response
8. **log** — record the response

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
  measure timing, status
  limit rate.ip(60/min)
  approve auth.authenticated

include admin.writ
include users.writ
include public.writ
```

### System Block

Defines defaults inherited by all handlers. Any step defined here applies everywhere unless overridden.

```
system ->
  log request, response
  measure timing, status
  limit rate.ip(60/min)
  approve auth.authenticated
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

Read data. Each resolve stores its result by name. Results can be passed as arguments to subsequent resolves.

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
  resolve input = parse_user(body)
  commit user = db.users.create(input)
  format user user.json
```

The pipeline never accesses fields on results. When a resolve or commit needs data from a previous step, the entire result object is passed and the function handles field access via type assertion.

Independent resolves with no data dependencies between them are executed in parallel automatically.

### Commit

Write data. Same syntax as resolve, but signals a state mutation. The pipeline can use this distinction for transaction boundaries.

```
commit user = db.users.create(input)
commit result = db.users.update(id, input)
commit result = db.users.delete(id)
```

Commit results are available to subsequent steps by name, just like resolve results.

### Format

Two parts: the data to format, and the registered formatter name.

```
format user user.show.json
format user,posts user.show.json
format status health.json
```

Multiple data sources are comma-separated. The formatter name is the registration key — it can encode any convention the developer wants (e.g., `user.show.json`, `user.list.json`).

### Errors

Error handling is defined in a separate block, scoped by route pattern. Errors are matched by Go type, with `default` as the catch-all.

```
errors /users/* ->
  DuplicateEmail  conflict.json
  NotFound        not_found.json
  Validation      validation.json
  default         error.json
```

The error block follows the same override model as the rest of the DSL:
- More specific route patterns win
- A system-level error block provides defaults
- Each entry maps a Go error type to a formatter

```
# system-level default
errors /* ->
  default error.json

# more specific handling for user routes
errors /users/* ->
  DuplicateEmail  conflict.json
  Validation      validation.json
  default         error.json
```

### Override Rules

For any given step type, the most specific declaration wins:
- Handler-level overrides group-level
- Group-level overrides system-level
- `none` explicitly removes an inherited step

## Migrations

First-class database migrations. Details TBD.

## Data Layer

Abstraction for database access with convention-based defaults and raw SQL escape hatch. Details TBD.

## Open Questions

- Code generation approach — `writ generate` producing typed Go glue code
- Middleware (CORS, request IDs, compression) — Writ's boundary vs standard Go middleware
- Response status code control (200 vs 201 vs 204)
- Data layer design — convention-based ORM vs explicit queries
- Migration tooling and workflow
- Testing patterns
- Configuration management (env vars, secrets, connection strings)
- HTML rendering approach (future)
