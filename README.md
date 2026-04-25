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
- Code generation over reflection — typed field access resolved at compile time, no runtime reflection
- Explicit value references — every value in the DSL uses `:` prefix to show where it comes from

## Pipeline Stages

Every request flows through these stages in order:

1. **log** — record that this happened
2. **measure** — instrument it
3. **session** — load session data from cookie or store
4. **csrf** — validate CSRF token on mutating HTML requests
5. **limit** — gate by rate, short-circuits with 429
6. **approve** — gate by permission, short-circuits with 401/403
7. **resolve** — read data, one or more steps, may depend on each other
8. **commit** — write data
9. **emit** — fire background events (runs after response)
10. **format** — shape the response (or **redirect** as alternative)
11. **log** — record the response

## DSL Syntax

### File Structure

- Encouraged to start as a single file
- `include` for organizational splitting, not a module system
- Runtime flattens all includes as if they were one file
- System block only lives in the root file

```text
# app.writ

system ->
  log request, response
  measure timing, status
  session cookie
  csrf auto
  limit rate.ip(60/min)
  approve auth.authenticated

include admin.writ
include users.writ
include public.writ
```

### System Block

Defines defaults inherited by all handlers. Any step defined here applies everywhere unless overridden.

```text
system ->
  log request, response
  measure timing, status
  session cookie
  csrf auto
  limit rate.ip(60/min)
  approve auth.authenticated
```

### Group Block

Overrides system defaults for a set of routes. Handlers within the group inherit group settings.

```text
group /admin/* ->
  approve auth.isAdmin
  limit rate.ip(10/min)
```

### Handler Block

Defines a single endpoint. Only declares what's unique to it. Inherits from group and system.

```text
GET /users/:id ->
  approve auth.isOwner(:id) OR auth.isAdmin
  resolve user = db.users(:id)
  resolve posts = db.posts(:user.id, limit=10)
  format user.show.json with user,posts
```

### Value References

Everything with `:` is a value reference. The DSL is fully explicit about where every value comes from:

- **`:id`** — route parameter, extracted from the route definition
- **`:user.id`** — field from a previous resolve/commit result, dot notation accesses the field
- **`limit=10`** — static value, named value defined in the DSL
- **`body CreateUserInput`** — typed request body, parsed and validated against the named Go struct
- **`query ListUsersQuery`** — typed query parameters, parsed and validated against the named Go struct

Field access with `:model.attribute` is resolved at code generation time. `writ generate` knows the return type of each resolver and produces direct typed accessors. No runtime reflection.

### Approve

Authorization gate. Supports `OR`, `AND`, `NOT` composition. Implementations are Go functions.

```text
approve auth.isOwner(:id) OR auth.isAdmin
approve auth.authenticated
approve none
```

### Resolve

Read data. Each resolve stores its result by name. Result fields can be passed as arguments to subsequent resolves using `:name.field` syntax.

```text
resolve user = db.users(:id)
resolve posts = db.posts(:user.id, limit=10)
resolve teammates = db.team_members(:user.team_id)
```

Resolvers receive simple values, not whole objects. This makes them simpler, more reusable, and decoupled from upstream types:

```go
// receives a string, not a User object
w.Resolver("db.team_members", func(ctx context.Context, params Params) (any, error) {
    teamID := params.String("team_id")
    return db.GetTeamMembers(teamID)
})
```

Independent resolves with no data dependencies between them are executed in parallel automatically.

### Commit

Write data. Same syntax as resolve, but signals a state mutation.

```text
commit user = db.users.create(body CreateUserInput)
commit user = db.users.update(:id, body UpdateUserInput)
commit db.users.delete(:id)
```

Typed `body` and `query` arguments work identically in commit and resolve. Commit results are available to subsequent steps by name, just like resolve results.

Multiple commits in a single handler are automatically wrapped in a database transaction. If any commit fails, everything rolls back.

### Emit

Fire background events after the response is sent. Non-blocking — the client doesn't wait.

```text
POST /users ->
  approve auth.isAdmin
  commit user = db.users.create(body CreateUserInput)
  emit user.created with user
  redirect /users/:user.id
```

The `with` keyword passes data to the event handlers. Multiple event handlers can listen to the same event:

```go
w.On("user.created", sendWelcomeEmail)
w.On("user.created", notifyAdminSlack)
w.On("user.created", trackAnalytics)
```

Event handler signature:

```go
func sendWelcomeEmail(ctx context.Context, data any) error {
    user := data.(User)
    return mailer.SendWelcome(user.Email, user.Name)
}
```

By default, events run in goroutines locally. For distributed messaging, register an emitter:

```go
// local goroutines (default, no registration needed)

// NATS — publishes to NATS subjects
w.Emitter(writ.NATS("NATS_URL"))

// NATS JetStream — durable, survives restarts
w.Emitter(writ.NATSJetStream("NATS_URL"))
```

Event names map directly to NATS subjects — `user.created` publishes to the `user.created` subject. The DSL doesn't change regardless of the emitter.

Listeners can run in the web process or in a separate worker:

```go
// separate worker process — no HTTP server, just event listeners
func main() {
    worker := writ.NewWorker()
    worker.Emitter(writ.NATS("NATS_URL"))

    worker.On("user.created", sendWelcomeEmail, writ.Queue("email-workers"))
    worker.On("user.created", notifyAdminSlack)
    worker.On("user.created", trackAnalytics)

    worker.Run()
}
```

`writ.NewWorker()` is a stripped-down Writ instance — no HTTP, no routes, just event listeners. `writ.Queue("email-workers")` uses NATS queue groups for load balancing across multiple worker instances.

### Format

The format line declares the template, the data, and optionally the layout:

```text
# HTML with default layout (app)
format user.show.html with user,posts

# HTML with explicit layout
format user.edit.html with user using layout admin

# HTML with different layout
format login.html with csrf using layout minimal

# JSON — no layout
format user.show.json with user,posts

# CSV — no layout
format users.export.csv with users

# Simple responses
format status.json with status
```

The pattern is:

- `format [template] with [data]` — default layout for HTML, no layout for other formats
- `format [template] with [data] using layout [name]` — override layout (HTML only)

Multiple data sources are comma-separated. The template name maps to the filesystem by convention (see [HTML Rendering](#html-rendering)).

The `using layout` keyword only applies to `.html` formats. Using it with `.json`, `.csv`, or any other format is a startup error.

### Content Negotiation

An endpoint can declare multiple format lines. The pipeline picks based on the Accept header:

```text
GET /users/:id ->
  approve auth.isOwner(:id) OR auth.isAdmin
  resolve user = db.users(:id)
  resolve posts = db.posts(:user.id, limit=10)
  format user.show.json with user,posts
  format user.show.html with user,posts
```

JSON for `application/json`, HTML for `text/html`. First listed is the default when the Accept header is ambiguous.

This keeps it explicit — you see exactly which formats an endpoint supports. No hidden negotiation.

### Redirect

Alternative to `format` for handlers that should redirect after processing. Common for Post/Redirect/Get pattern in web apps.

```text
POST /users ->
  approve auth.isAdmin
  commit user = db.users.create(body CreateUserInput)
  emit user.created with user
  redirect /users/:user.id

PUT /users/:id ->
  approve auth.isOwner(:id)
  commit user = db.users.update(:id, body UpdateUserInput)
  redirect /users/:user.id

DELETE /users/:id ->
  approve auth.isOwner(:id)
  commit db.users.delete(:id)
  redirect /users
```

Redirect URLs use the same `:` reference syntax as the rest of the DSL:

- `:id` — route parameter
- `:user.id` — field from a commit result

Status codes are conventional:

- `redirect` after `commit` → **303 See Other**
- `redirect` without `commit` → **302 Found**

A handler ends with `format`, `redirect`, or both (content negotiation). `redirect` and `format` cannot be mixed — a handler either formats a response or redirects, unless multiple format lines are used for content negotiation.

### Layout

Declares the HTML layout template for a group or handler. Only applies to `.html` formats. Follows the override model — handler beats group beats system default of `app`.

```text
group /admin/* ->
  layout admin

GET /login ->
  format login.html with csrf using layout minimal
```

### Session

Loads session data before approve steps run. Declared at system level:

```text
system ->
  session cookie
```

Session data is available to approvers and resolvers through context:

```go
w.Approver("auth.authenticated", func(ctx context.Context, req Request) (bool, error) {
    session := writ.GetSession(ctx)
    return session.Has("user_id"), nil
})

w.Approver("auth.isOwner", func(ctx context.Context, req Request) (bool, error) {
    session := writ.GetSession(ctx)
    return session.Get("user_id") == req.Param("id"), nil
})
```

Login and logout are commit handlers that write to the session:

```text
POST /login ->
  approve none
  commit session = auth.login(body LoginInput)
  redirect /dashboard

POST /logout ->
  commit auth.logout()
  redirect /login
```

```go
w.Commit("auth.login", func(ctx context.Context, params Params) (any, error) {
    input := params.Get("body").(LoginInput)
    user, err := db.Authenticate(input.Email, input.Password)
    if err != nil {
        return nil, Unauthorized{Message: "invalid credentials"}
    }
    session := writ.GetSession(ctx)
    session.Set("user_id", user.ID)
    session.Set("role", user.Role)
    return user, nil
})

w.Commit("auth.logout", func(ctx context.Context, params Params) (any, error) {
    session := writ.GetSession(ctx)
    session.Clear()
    return nil, nil
})
```

Session storage follows the source convention:

```go
w.Session(writ.CookieSession("SECRET_KEY"))     // encrypted cookie (default)
w.Session(writ.RedisSession(writ.Redis()))      // server-side with Redis
w.Session(writ.DBSession(writ.Postgres()))      // server-side with database
```

### CSRF Protection

Automatic for HTML forms. Piggybacks on the session system:

```text
system ->
  session cookie
  csrf auto
```

`csrf auto` means:

- Every session gets a CSRF token automatically
- Every POST/PUT/DELETE on an HTML route validates the token
- Every HTML template has `{{ .CSRFToken }}` and `{{ .CSRFField }}` available automatically
- JSON API routes skip CSRF (they use token auth, not cookies)

```html
<form method="POST" action="/users/{{ .User.ID }}">
  {{ .CSRFField }}
  <input type="text" name="name" value="{{ .User.Name }}">
  <button type="submit">Save</button>
</form>
```

`{{ .CSRFField }}` renders as `<input type="hidden" name="_csrf" value="...">`.

If the token is missing or invalid, the pipeline short-circuits with 403 before any resolve or commit runs. No handler code needed.

### Errors

Error handling is defined in a separate block, scoped by route pattern. Errors are matched by Go type, with `default` as the catch-all.

```text
errors /* ->
  default error.json

errors /users/* ->
  DuplicateEmail  conflict.json
  NotFound        not_found.json
  Validation      validation.json
  default         error.json

errors /admin/* ->
  Forbidden       forbidden.html
  default         error.html
```

The error block follows the same override model as the rest of the DSL:

- More specific route patterns win
- A system-level error block provides defaults
- Each entry maps a Go error type to a formatter

### Override Rules

For any given step type, the most specific declaration wins:

- Handler-level overrides group-level
- Group-level overrides system-level
- `none` explicitly removes an inherited step

## Data Layer

### SQL as Resolvers

Resolvers and commits can be defined as named SQL queries in `.sql` files instead of Go functions. The pipeline reads the SQL, binds parameters, executes the query, and scans the result — no Go boilerplate needed for standard data access.

```sql
-- queries/users.sql

-- name: db.users
SELECT id, name, email FROM users WHERE id = $id;

-- name: db.users.list
SELECT id, name, email FROM users
ORDER BY $sort LIMIT $limit OFFSET $offset;

-- name: db.users.create
INSERT INTO users (name, email, password)
VALUES ($name, $email, $password)
RETURNING id, name, email;

-- name: db.users.update
UPDATE users SET name = $name, email = $email
WHERE id = $id
RETURNING id, name, email;

-- name: db.users.delete
DELETE FROM users WHERE id = $id;
```

The `-- name:` comment maps each query to the resolver name used in the DSL. The pipeline matches `db.users.create` in the `.writ` file to the named SQL query automatically.

### Parameter Binding Convention

SQL parameters use `$name` syntax. The pipeline binds them to input struct fields by matching the lowercase parameter name to the struct field name:

```text
$name   →  CreateUserInput.Name
$email  →  CreateUserInput.Email
$id     →  route parameter :id
```

No struct tags needed for binding. The convention is: lowercase the Go field name, match the SQL parameter name.

### Result Scanning Convention

The resolve variable name implies the Go type for scanning:

```text
resolve user = db.users(:id)                        →  scans into User
resolve posts = db.posts(:user.id)                  →  scans into []Post
resolve teammates = db.team_members(:user.team_id)  →  scans into []Teammate
```

The convention is: capitalize the variable name to get the type. Plural variable names scan into slices. The pipeline maps SQL column names to struct field names using the same lowercase matching convention.

### Connection Convention

SQL files in the `queries/` directory run against the default database source. Subdirectories map to named sources:

```text
queries/
  users.sql          → runs against default source ("db")
  posts.sql          → runs against default source ("db")
  cache/
    sessions.sql     → runs against "cache" source
```

### Custom Resolver Override

If a Go resolver is registered with the same name as a SQL query, the Go resolver wins. SQL is the default, Go is the escape hatch:

```go
// overrides the SQL query named db.users
w.Resolver("db.users", func(ctx context.Context, params Params) (any, error) {
    // custom logic, complex joins, external API calls, etc.
})
```

### Transactions

Multiple commits in a single handler are automatically wrapped in a database transaction:

```text
PUT /users/:id/transfer ->
  commit debit = db.accounts.debit(:id, body TransferInput)
  commit credit = db.accounts.credit(body TransferInput)
```

If either commit fails, both roll back. No explicit transaction management needed. The pipeline handles begin, commit, and rollback.

## Typed Input Structs

Body and query types are standard Go structs with tags for validation and defaults.

### JSON Bodies

Structs with `json` tags are parsed as JSON:

```go
type CreateUserInput struct {
    Email    string `json:"email"    validate:"required,email"`
    Name     string `json:"name"     validate:"required,min=2"`
    Password string `json:"password" validate:"required,min=8"`
}

type UpdateUserInput struct {
    Email string `json:"email" validate:"omitempty,email"`
    Name  string `json:"name"  validate:"omitempty,min=2"`
}
```

### Form and File Uploads

Structs with `form` tags are parsed as `multipart/form-data`:

```go
type AvatarUpload struct {
    File writ.File `form:"avatar" validate:"required,max=5mb,types=jpg,png"`
}

type CreatePostInput struct {
    Title string    `form:"title" validate:"required"`
    Body  string    `form:"body"  validate:"required"`
    Image writ.File `form:"image" validate:"max=10mb,types=jpg,png,webp"`
}
```

The convention: if the struct has `form` tags, parse as multipart. If it has `json` tags, parse as JSON. The presence of `writ.File` fields signals file upload support.

File uploads in the DSL look the same as any other body:

```text
POST /users/:id/avatar ->
  approve auth.isOwner(:id)
  commit avatar = files.upload(body AvatarUpload)
  redirect /users/:id
```

```go
w.Commit("files.upload", func(ctx context.Context, params Params) (any, error) {
    upload := params.Get("body").(AvatarUpload)
    return storage.Save(upload.File)
})
```

### Query Parameters

```go
type ListUsersQuery struct {
    Role  string `query:"role"`
    Page  int    `query:"page"   validate:"min=1"        default:"1"`
    Limit int    `query:"limit"  validate:"min=1,max=100" default:"20"`
    Sort  string `query:"sort"   default:"id"`
}
```

### Validation

The pipeline uses struct tags to:

- Parse the request body (JSON or multipart) or query parameters into the struct
- Apply default values for missing fields
- Validate all fields according to the `validate` tag rules
- Enforce file size and type constraints
- Short-circuit with a `Validation` error if any check fails

## Response Status Codes

The pipeline infers status codes from context:

- Successful resolve + format → **200**
- Successful commit + format → **201**
- Successful delete commit → **204**
- Redirect after commit → **303 See Other**
- Redirect without commit → **302 Found**
- Approve failure → **401** or **403**
- Limit failure → **429**
- CSRF failure → **403**
- Validation failure → status from error type

Error types carry their own status via a `StatusCode()` method. The pipeline checks for this method on returned errors. If present, it uses that. If not, it defaults to 500.

For overriding success codes, the formatter can signal it:

```go
func myFormatter(ctx context.Context, data any) ([]byte, error) {
    writ.SetStatus(ctx, 202)
    return json.Marshal(map[string]string{"status": "accepted"})
}
```

## Code Generation

Writ uses code generation instead of reflection. Running `writ generate` reads the `.writ` files and SQL files and produces typed Go glue code.

### What Gets Generated

- Route registration mapping each handler to its pipeline stages
- Typed field accessors for `:model.attribute` references
- SQL parameter binding and result scanning code from named queries
- Compile-time verification that all referenced names are registered
- Compile-time verification that all field references are valid

### What Stays Handwritten

- Custom resolver, commit, approver, formatter, and limiter implementations
- Event handler implementations
- Error type definitions
- Input structs (body and query types)
- Model/struct definitions
- SQL query files
- Application configuration and startup

### Workflow

```text
writ generate          # reads *.writ + *.sql, produces Go files
go build               # compile-time catches any mismatches
go run .               # start the server
```

The generated code is committed to the repo — it's readable, debuggable, and diff-able.

## Startup Validation

When the server starts, the pipeline validates:

- Every resolver, approver, formatter, limiter, and error handler referenced in the DSL is registered (either as SQL or Go)
- Route parameters referenced in resolve/commit arguments exist in the route definition
- Field references (`:model.attribute`) are valid for the return type of the referenced resolve/commit
- SQL parameter names match input struct fields
- SQL column names match output struct fields
- Resolve/commit dependencies form a valid DAG (no circular references)
- Body and query type names correspond to registered Go types
- Every `.html` template referenced in a format line exists on disk
- Every layout referenced by `using layout`, group `layout`, or the system default exists in `templates/layouts/`
- `using layout` is not used with non-HTML formats
- A handler ends with `format`, `redirect`, or multiple `format` lines for content negotiation
- `redirect` and `format` are not mixed on the same handler (except multiple format lines)
- Event names in `emit` have at least one registered handler
- `csrf auto` requires `session` to be configured
- Include files exist and parse correctly
- Error type names in error blocks correspond to registered Go types

All wiring errors are caught before a single request is served.

## HTML Rendering

### Template Convention

The template name in the format line maps directly to the filesystem:

```text
format user.show.html with user   →  templates/user/show.html
format user.list.html with users  →  templates/user/list.html
format login.html with csrf       →  templates/login.html
```

Dots become path separators, with the final `.html` as the extension.

### Layouts

HTML templates render inside a layout. The default layout is `templates/layouts/app.html`.

```html
<!-- templates/layouts/app.html -->
<!DOCTYPE html>
<html>
<head>
  <title>{{ .Title }}</title>
</head>
<body>
  <nav>...</nav>
  {{ template "content" . }}
  <footer>...</footer>
</body>
</html>
```

Templates define the `content` block:

```html
<!-- templates/user/show.html -->
{{ define "content" }}
<div class="profile">
  <h1>{{ .User.Name }}</h1>
  <p>{{ .User.Email }}</p>

  <h2>Posts</h2>
  {{ range .Posts }}
    <article>
      <h3>{{ .Title }}</h3>
      <p>{{ .Body }}</p>
    </article>
  {{ end }}
</div>
{{ end }}
```

### Layout Inheritance

Layouts follow the same override model as everything else — handler beats group beats system default:

```text
group /admin/* ->
  approve auth.isAdmin
  layout admin

GET /admin/users ->
  resolve users = db.users.list(query ListUsersQuery)
  format users.list.html with users

GET /login ->
  approve none
  resolve csrf = auth.csrf_token()
  format login.html with csrf using layout minimal
```

### Template Data

Resolve names are capitalized and passed as template context:

```text
resolve user = db.users(:id)        →  {{ .User }}
resolve posts = db.posts(:user.id)  →  {{ .Posts }}
```

CSRF token and field are automatically available in all HTML templates when `csrf auto` is configured:

- `{{ .CSRFToken }}` — the raw token value
- `{{ .CSRFField }}` — renders as `<input type="hidden" name="_csrf" value="...">`

### Auto-Generated Formatters

If a template file exists on disk, the corresponding formatter is automatically available. No Go registration needed for standard template rendering. Custom formatters still override the automatic ones when more control is needed.

### Static Assets

Static files are served from the `public/` directory automatically:

```text
public/
  css/
    app.css
  js/
    app.js
  images/
    logo.png
```

These are available at `/css/app.css`, `/js/app.js`, etc.

## Testing

Tests are written in the same DSL as the app. No Go code needed for the common cases.

**File extension:** `.test.writ`

### Test Structure

```text
# tests/users.test.writ

users ->
  owner {role: "user", id: "user-123"}
  admin {role: "admin", id: "admin-1"}
  stranger {role: "user", id: "user-456"}

fixtures ->
  new_user {"name": "Alice", "email": "alice@example.com", "password": "secure123"}
  update_name {"name": "Alice Updated"}
  duplicate_email {"name": "Fake", "email": "alice@example.com", "password": "pass1234"}

test "create user" ->
  as admin POST /users with new_user
  expect status 201
  expect body.name "Alice"
  capture alice_id = body.id

test "get user" ->
  as owner GET /users/$alice_id
  expect status 200
  expect body.email "alice@example.com"

test "unauthorized access" ->
  as stranger GET /users/$alice_id
  expect status 403

test "update user" ->
  as owner PUT /users/$alice_id with update_name
  expect status 200
  expect body.name "Alice Updated"

test "duplicate email" ->
  as admin POST /users with duplicate_email
  expect status 409

test "delete user" ->
  as owner DELETE /users/$alice_id
  expect status 204

test "get deleted user" ->
  as admin GET /users/$alice_id
  expect status 404
```

### Request Line Format

Every request line follows the same pattern:

- `as [user] [METHOD] [path]` — for requests without a body
- `as [user] [METHOD] [path] with [fixture]` — for requests with a body

`users` defines identities for authentication. `fixtures` defines named payloads. The request line brings them together.

### Capture

The `capture` keyword saves a value from a response for use in subsequent tests:

```text
test "create user" ->
  as admin POST /users with new_user
  capture alice_id = body.id

test "get user" ->
  as owner GET /users/$alice_id
  expect status 200
```

Captured values are referenced with `$` prefix and are available to all subsequent tests in the same file.

### Seed Data

For reference data that tests depend on, seed files load SQL before tests run:

```text
# tests/admin.test.writ

seed testdata/roles.sql
seed testdata/categories.sql

users ->
  admin {role: "admin", id: "admin-1"}

test "list categories" ->
  as admin GET /categories
  expect status 200
```

### Execution

The entire test suite runs against a fresh database. Migrations run first, then seed data loads, then tests execute in order. Tests build on each other — each test inherits the database state left by the previous one.

```text
$ writ test

  migrate   ✓ (12 migrations)

  tests/users.test.writ
    ✓ create user
    ✓ get user
    ✓ unauthorized access
    ✓ update user
    ✓ duplicate email
    ✓ delete user
    ✓ get deleted user

  7 passed, 0 failed
```

### Go Tests

For anything the DSL can't express — complex assertions, performance tests, concurrency tests — standard Go tests work. Writ provides test helpers:

- `writ.TestPipeline(t, "app.writ")` — parse a `.writ` file and execute requests without HTTP
- `writ.TestDB(t, connString)` — connect to a test database
- `writ.MockRequest(...)` — build a fake request for unit testing individual functions

Unit testing resolvers, approvers, and formatters is just testing Go functions — no framework involvement needed.

## Migrations

Plain SQL files with timestamp prefixes. No Go code, no DSL — just SQL.

### File Structure

```text
migrations/
  20260315102300_create_users.sql
  20260315102301_create_posts.sql
  20260420140533_add_user_role.sql
  20260424091200_create_teams.sql
```

Timestamp format is `YYYYMMDDHHMMSS`. This avoids collisions when multiple developers create migrations on different branches.

### Migration File Format

Each file has up and down sections separated by a comment marker:

```sql
-- migrate: up

CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL,
  email VARCHAR(255) UNIQUE NOT NULL,
  password VARCHAR(255) NOT NULL,
  role VARCHAR(50) DEFAULT 'user',
  created_at TIMESTAMPTZ DEFAULT now(),
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_users_email ON users(email);

-- migrate: down

DROP TABLE users;
```

### Tracking

Writ tracks applied migrations in a `writ_migrations` table in the database.

### CLI

```text
$ writ migrate new add_user_avatar

  created migrations/20260424153012_add_user_avatar.sql

$ writ migrate up

  20260315102300_create_users      ✓
  20260315102301_create_posts      ✓
  20260420140533_add_user_role     ✓ (new)
  20260424091200_create_teams      ✓ (new)

  2 migrations applied

$ writ migrate down

  20260424091200_create_teams      ✓ rolled back

$ writ migrate status

  20260315102300_create_users      applied  2026-03-15 10:23:01
  20260315102301_create_posts      applied  2026-03-15 10:23:01
  20260420140533_add_user_role     applied  2026-04-20 14:05:33
  20260424091200_create_teams      pending
```

### Testing Integration

When `writ test` runs, it creates a fresh database, applies all migrations, then executes tests:

```text
$ writ test

  migrate
    20260315102300_create_users      ✓
    20260315102301_create_posts      ✓
    20260420140533_add_user_role     ✓
    20260424091200_create_teams      ✓

  tests/users.test.writ
    ✓ create user
    ...
```

## Configuration

Environment variables, twelve-factor style. No config files, no YAML, no TOML.

### Source Convention

Source names map to environment variables by convention — source name uppercased plus `_URL`:

- `db` → `DATABASE_URL` (special case, industry standard)
- `cache` → `CACHE_URL`
- `search` → `SEARCH_URL`

```go
w := writ.New()

// reads DATABASE_URL automatically
w.Source("db", writ.Postgres())

// reads CACHE_URL automatically
w.Source("cache", writ.Redis())

w.Run("app.writ")
```

### Port

Writ reads `PORT` automatically. Defaults to `8080` if not set.

```text
PORT=3000 writ run
```

### Custom Config

For values that resolvers need, `w.Config` maps a name to an environment variable:

```go
w.Config("stripe_key", "STRIPE_SECRET_KEY")
```

Accessed inside resolvers through context:

```go
w.Resolver("payments.charge", func(ctx context.Context, params Params) (any, error) {
    key := writ.GetConfig(ctx, "stripe_key")
    // ...
})
```

If the environment variable is missing at startup, Writ fails with a clear error. No runtime surprises.

### Environment

Writ reads `WRIT_ENV` to adjust behavior:

- `development` — verbose logging, detailed errors, hot reload
- `test` — used by `writ test`
- `production` — minimal logging, generic errors

### Local Development

A `.env` file in the project root is loaded automatically in development:

```text
DATABASE_URL=postgres://localhost/myapp
NATS_URL=nats://localhost:4222
CACHE_URL=redis://localhost:6379
PORT=8080
WRIT_ENV=development
SECRET_KEY=dev-secret
STRIPE_SECRET_KEY=sk_test_xxx
```

## Development Mode

### Hot Reload

`writ dev` watches the filesystem and restarts automatically on changes:

```text
$ writ dev

  watching app.writ, queries/, templates/, *.go
  server started on :8080

  [change detected] templates/user/show.html
  server restarted on :8080
```

Watched paths:

- `.writ` files — full restart
- `.sql` query files — full restart
- `.html` templates — hot reload without restart (templates loaded per-request in development)
- `.go` source files — full rebuild and restart

`writ dev` replaces `writ run` during development. In production, use `writ run` or the compiled binary directly.

## Full Example

### Project Structure

```text
myapp/
  app.writ
  main.go
  .env
  migrations/
    20260315102300_create_users.sql
    20260315102301_create_posts.sql
  queries/
    users.sql
    posts.sql
  templates/
    layouts/
      app.html
      admin.html
      minimal.html
    user/
      show.html
      list.html
      edit.html
    post/
      show.html
    error/
      404.html
      500.html
    login.html
  public/
    css/
      app.css
    js/
      app.js
    images/
      logo.png
  tests/
    users.test.writ
  types/
    models.go
    inputs.go
    errors.go
```

### app.writ

```text
system ->
  log request, response
  measure timing, status
  session cookie
  csrf auto
  limit rate.ip(60/min)
  approve auth.authenticated

errors /* ->
  default error.json

errors /users/* ->
  DuplicateEmail  conflict.json
  NotFound        not_found.json
  Validation      validation.json
  default         error.json

POST /login ->
  approve none
  csrf none
  commit session = auth.login(body LoginInput)
  redirect /dashboard

POST /logout ->
  commit auth.logout()
  redirect /login

GET /health ->
  approve none
  limit none
  session none
  csrf none
  resolve status = sys.health()
  format health.json with status

GET /users ->
  resolve users = db.users.list(query ListUsersQuery)
  format users.list.json with users
  format users.list.html with users

GET /users/:id ->
  approve auth.isOwner(:id) OR auth.isAdmin
  resolve user = db.users(:id)
  resolve posts = db.posts(:user.id, limit=10)
  format user.show.json with user,posts
  format user.show.html with user,posts

POST /users ->
  approve auth.isAdmin
  commit user = db.users.create(body CreateUserInput)
  emit user.created with user
  redirect /users/:user.id

PUT /users/:id ->
  approve auth.isOwner(:id) OR auth.isAdmin
  commit user = db.users.update(:id, body UpdateUserInput)
  redirect /users/:user.id

DELETE /users/:id ->
  approve auth.isOwner(:id) OR auth.isAdmin
  commit db.users.delete(:id)
  redirect /users

POST /users/:id/avatar ->
  approve auth.isOwner(:id)
  commit avatar = files.upload(body AvatarUpload)
  redirect /users/:id

GET /users/:id/team ->
  approve auth.isOwner(:id) OR auth.isAdmin
  resolve user = db.users(:id)
  resolve teammates = db.team_members(:user.team_id)
  format team.show.json with user,teammates
  format team.show.html with user,teammates
```

### queries/users.sql

```sql
-- name: db.users
SELECT id, name, email FROM users WHERE id = $id;

-- name: db.users.list
SELECT id, name, email FROM users
ORDER BY $sort LIMIT $limit OFFSET $offset;

-- name: db.users.create
INSERT INTO users (name, email, password)
VALUES ($name, $email, $password)
RETURNING id, name, email;

-- name: db.users.update
UPDATE users SET name = $name, email = $email
WHERE id = $id
RETURNING id, name, email;

-- name: db.users.delete
DELETE FROM users WHERE id = $id;
```

### types/models.go

```go
type User struct {
    ID    string
    Name  string
    Email string
}

type Post struct {
    ID     string
    UserID string
    Title  string
    Body   string
}
```

### types/inputs.go

```go
type CreateUserInput struct {
    Email    string `json:"email"    validate:"required,email"`
    Name     string `json:"name"     validate:"required,min=2"`
    Password string `json:"password" validate:"required,min=8"`
}

type UpdateUserInput struct {
    Email string `json:"email" validate:"omitempty,email"`
    Name  string `json:"name"  validate:"omitempty,min=2"`
}

type LoginInput struct {
    Email    string `json:"email"    validate:"required,email"`
    Password string `json:"password" validate:"required"`
}

type AvatarUpload struct {
    File writ.File `form:"avatar" validate:"required,max=5mb,types=jpg,png"`
}

type ListUsersQuery struct {
    Role  string `query:"role"`
    Page  int    `query:"page"   validate:"min=1"        default:"1"`
    Limit int    `query:"limit"  validate:"min=1,max=100" default:"20"`
    Sort  string `query:"sort"   default:"id"`
}

type PostFilters struct {
    Status string `query:"status"`
    Limit  int    `query:"limit" validate:"min=1,max=50" default:"10"`
}
```

### types/errors.go

```go
type NotFound struct{ Resource string }
func (e NotFound) StatusCode() int { return 404 }

type Validation struct{ Fields map[string]string }
func (e Validation) StatusCode() int { return 422 }

type DuplicateEmail struct{ Email string }
func (e DuplicateEmail) StatusCode() int { return 409 }

type Unauthorized struct{ Message string }
func (e Unauthorized) StatusCode() int { return 401 }
```

### main.go

```go
package main

import "github.com/stonean/writ"

func main() {
    w := writ.New()

    // data source — reads DATABASE_URL automatically
    w.Source("db", writ.Postgres())

    // session — reads SECRET_KEY automatically
    w.Session(writ.CookieSession("SECRET_KEY"))

    // emitter — reads NATS_URL automatically (optional, defaults to goroutines)
    w.Emitter(writ.NATS("NATS_URL"))

    // approvers
    w.Approver("auth.authenticated", authCheck)
    w.Approver("auth.isOwner", ownerCheck)
    w.Approver("auth.isAdmin", adminCheck)

    // limiters
    w.Limiter("rate.ip", ipRateLimiter)

    // custom resolvers (only what SQL can't handle)
    w.Resolver("sys.health", healthResolver)
    w.Resolver("db.team_members", teamMembersResolver)

    // commits
    w.Commit("auth.login", loginHandler)
    w.Commit("auth.logout", logoutHandler)
    w.Commit("files.upload", uploadHandler)

    // background event handlers
    w.On("user.created", sendWelcomeEmail)
    w.On("user.created", notifyAdminSlack)

    w.Run("app.writ")
}
```

## CLI Summary

```text
writ generate          # reads *.writ + *.sql, produces Go files
writ run               # start the server (production)
writ dev               # start with hot reload (development)
writ worker            # start a background worker (event listeners only)
writ test              # run test suite against fresh database
writ show [route]      # display effective pipeline for a route
writ routes            # list all defined routes
writ migrate new       # create a new migration file
writ migrate up        # apply pending migrations
writ migrate down      # roll back last migration
writ migrate status    # show migration status
```

## Middleware Boundary

Writ owns the request lifecycle as defined by the pipeline stages. Standard cross-cutting concerns that are not request-specific live outside Writ as standard Go middleware:

**Outside Writ (standard Go middleware):**

- CORS
- Request ID generation
- Compression (gzip)
- Panic recovery
- TLS/HTTPS redirect

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

The boundary is: if it cares about the business logic or data flow, it's a Writ stage. If it's pure infrastructure, it's standard middleware that wraps the Writ handler.

```go
handler := cors.Default().Handler(
    requestid.Handler(
        w.Handler(),
    ),
)
http.ListenAndServe(":8080", handler)
```

## Design Decisions

- **No runtime reflection** — all type access resolved at code generation time
- **Explicit value references** — `:model.attribute` syntax replaced whole-object passing, simplifying resolver signatures and eliminating type coupling between resolvers
- **Response validation** — considered and rejected. The formatter already controls what goes out. Adding a validation layer on top is redundant complexity. Write good formatters.
- **SQL as resolvers** — write SQL directly instead of generating it from Go code. If Postgres can run it, Writ can use it. No intermediate ORM layer.
- **Convention over configuration** — timestamps for migrations, directory structure for connections, env vars for config, struct names for type scanning, tag types for parsing. One way to do things.
- **`form` vs `json` tags** — the struct tag type determines the parsing strategy. No ambiguity, no configuration.
- **CSRF automatic for HTML** — no opt-in, no forgotten tokens. JSON APIs skip it by design.
- **Background events** — `emit` defaults to goroutines for simplicity, NATS for distributed messaging. The DSL stays the same regardless of the transport. `writ.NewWorker()` enables separate worker processes with the same event handler signatures.

## Project Governance

Writ follows a spec-driven development pipeline. New features are defined as specs, planned, broken into tasks, and then implemented.

- [`constitution.md`](constitution.md) — guiding principles, pipeline (spec → plan → tasks → implement), spec lifecycle, quality gates
- [`AGENTS.md`](AGENTS.md) — agent rules: tech stack, conventions, DSL boundaries, project structure
- [`specs/`](specs/) — feature specs and project-wide architecture
  - [`system.md`](specs/system.md) — cross-cutting architecture (configuration, lifecycle, request flow, middleware boundary)
  - [`events.md`](specs/events.md) — event conventions and catalog
  - [`errors.md`](specs/errors.md) — error conventions and catalog
  - [`inbox.md`](specs/inbox.md) — feature backlog awaiting specs

## Features

| # | Feature | Status | Spec |
| --- | --- | --- | --- |
| 001 | DSL Parser | clarified | [spec](specs/001-dsl-parser/spec.md) |

The Writ surface above is the design target. See [`specs/inbox.md`](specs/inbox.md) for the backlog of feature areas waiting to be specced.
