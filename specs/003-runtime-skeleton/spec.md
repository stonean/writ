# 003 — Runtime Skeleton + HTTP Dispatch

**Status:** planned
**Dependencies:** 001-dsl-parser, 002-pipeline-elaboration

The runtime skeleton is the first end-to-end execution path through Writ: parse a `.writ` file, elaborate the pipeline, register Go resolvers and formatters, bind an HTTP listener, dispatch incoming requests to the right handler, run the resolve and format stages, and write the response. It is the smallest amount of code that proves the framework actually serves a request.

This spec deliberately strips the runtime down to two pipeline stages — `resolve` and `format` — and defers every other stage, every value-reference shape beyond bare route parameters, every higher-level convenience (templates, code generation, typed bodies, content negotiation, transactions, workers), and every CLI verb beyond starting a process. Later features layer those on without having to redesign the runtime's bones.

## In Scope

The runtime serves a request end-to-end when:

- A `.writ` file declares one or more handlers.
- Each handler's effective pipeline (per spec 002) contains zero or more `resolve` steps and exactly one `format` step. A handler with `commit`, `emit`, `approve`, `session`, `csrf`, `limit`, `redirect`, `layout`, `log`, or `measure` in its effective pipeline is rejected at startup with a clear error naming the unsupported stage.
- All resolver names referenced in the DSL map to Go functions registered through the runtime's registration API. Likewise for formatter names.
- Route parameters (`:id`, `:slug`) are the only value-reference shape used in resolver and formatter argument lists. Field references (`:user.id`), typed bodies (`body T`), typed queries (`query T`), and named-argument literals (`limit=10`) are out of scope and rejected at startup.

A handler outside that envelope is a startup error rather than a runtime surprise.

## Out of Scope

Each item below has its own future feature spec. The runtime skeleton's job is to leave room for them, not to ship them.

- All pipeline stages other than `resolve` and `format`: `log`, `measure`, `session`, `csrf`, `limit`, `approve`, `commit`, `emit`, `layout`, `redirect`.
- Source registration (`w.Source`), session backends, emitter backends, custom config (`w.Config`), event listeners (`w.On`).
- HTML templates and the auto-generated formatter convention. Every formatter in this iteration is a registered Go function; there is no on-disk template lookup.
- Field references (`:user.id`), typed body/query inputs, named-argument literals, rate-limit literals.
- SQL resolvers and the `queries/` directory convention.
- Code generation (`writ generate`). The runtime resolves names at startup and dispatches by registered name; typed glue is added by a future feature.
- Transactions, parallel resolve execution, content negotiation, multiple formats per handler, status-code inference beyond the trivial defaults.
- The `errors` block. Resolver and formatter errors short-circuit with a default 500 response and a generic body.
- CLI surface beyond a single program entry. `writ generate`, `writ dev`, `writ test`, `writ migrate`, `writ show`, `writ routes`, `writ worker` are out of scope.
- Worker processes (`writ.NewWorker()`).
- Hot reload, file watchers, multiple `.writ` source roots.
- Dedicated test-helper surface (`writ.TestPipeline`, `writ.TestDB`, `writ.MockRequest`). Standard testing uses `Load` + `Handler` with `httptest`. Tracked in `specs/inbox.md` under the **Testing DSL** item, which will own dedicated helpers when a real test consumer drives their shape.

## Public API Surface

The runtime exposes a single Go package. The exact spelling is refined in the plan phase, but the contract sketched here is the surface every consumer relies on:

- A constructor that builds a fresh runtime instance with no registrations.
- A registration method for resolvers, taking a name and a Go function.
- A registration method for formatters, taking a name and a Go function.
- A loader method that reads a `.writ` file path, parses it, elaborates the pipeline, runs startup validation, and returns either success or a structured error.
- A method that returns the runtime as a Go HTTP handler — usable from `http.ListenAndServe` and from third-party middleware (the system spec's "outside Writ" boundary).
- A method that runs an HTTP listener on the configured port and blocks until the process is interrupted. This is a thin convenience over the previous two.

Registration is order-independent — registrations made before or after loading the `.writ` file are equivalent. Loading must happen before serving begins.

## Resolver Contract

A resolver is a Go function that reads inputs and returns a single value plus an optional error. The contract:

- Inputs available to a resolver: the request context, plus any route parameters that the DSL passed it as `:name` arguments.
- The resolver returns one untyped result value and an error. Returning a non-nil error short-circuits the request with a generic 500 response.
- The result value is stored under the resolve step's variable name in the request's named-result table (e.g., `resolve user = db.users(:id)` stores the result under the key `user`).
- Resolvers in this iteration run sequentially, in the canonical order produced by spec 002. Parallel execution and dependency-DAG analysis are out of scope.

## Formatter Contract

A formatter is a Go function that turns one or more named result values into a response body. The contract:

- Inputs available to a formatter: the request context; the `http.ResponseWriter`; and the named-result table populated by the `resolve` steps, restricted to the names the format line listed in its `with` clause.
- The formatter is responsible for writing the response status, headers, and body. The runtime never touches response headers on the success path; if the formatter does not set `Content-Type`, `http.ResponseWriter` falls back to `http.DetectContentType` on the first write per `net/http` defaults.
- Returning an error from a formatter short-circuits with a generic 500 response, but only if the formatter has not already written a status. (Formatters that have already started writing the response cannot be unwound.)
- Formatters do not receive route parameters directly. If a formatter needs a route parameter, a `resolve` step must capture it first.

## Application Lifecycle

The runtime's startup sequence (matching `system.md` *Startup Sequence*, restricted to in-scope concerns):

1. Read environment variables (no `.env` file loading in this iteration).
2. Construct the runtime instance and accept registrations.
3. Read the `.writ` file at the path supplied to the loader, parse it, flatten includes per spec 001.
4. Elaborate the pipeline per spec 002.
5. Run startup validation (see *Startup Validation* below). On any failure, return a single structured error that aggregates every problem found, in source order.
6. On success, the runtime is ready to serve. The HTTP listener is bound only when the convenience method is called; consumers that use `Handler()` directly bind their own listener.

The runtime never reads `.writ` files at request time and never re-elaborates the pipeline at request time.

`Load()` is a startup-time, one-shot operation. Calling `Load()` concurrently with another in-flight `Load()` is a programming error and produces a runtime panic with a clear message; the runtime does not serialize or "first wins" silently. After a successful load, a second `Load()` call returns the "already loaded" error per the routing acceptance criteria.

## Startup Validation

Of the validation set listed in `system.md` *Startup Validation*, the runtime skeleton enforces only the items that the in-scope DSL surface can produce:

- **Reference completeness (resolvers, formatters)** — every resolver name referenced in any handler's effective pipeline is registered. Likewise for every formatter name. Unregistered names list the handler and source span.
- **Pipeline shape (skeleton subset)** — every handler's effective pipeline ends in exactly one `format`, and contains zero or more `resolve` steps and no other stage kinds. Any handler whose effective pipeline includes an out-of-scope stage is reported as "stage X not supported in skeleton runtime" with the source span of the offending declaration.
- **Route-parameter completeness** — every `:name` referenced in a resolver argument list resolves to a parameter declared in the handler's route pattern. References to undeclared parameters are reported with the source span of the referencing argument.
- **Configuration** — every required environment variable resolves to a usable value. The reserved variables in scope are `PORT` (default `8080`) and `WRIT_ENV` (default `production`); custom config (`w.Config`) is out of scope.

Items outside the runtime's surface — field references, typed bodies, layouts, templates, dependency cycles, body/query types, event listeners, SQL parameters, error type names — are not checked because they cannot occur in an in-scope program.

All validation runs before the listener binds. A startup error never reaches a client.

## Startup Error Type

Following the convention established by spec 001 (`parser.Error`) and spec 002 (`pipeline.Error`), the runtime defines a structured error type for startup failures with an exported `Kind` field consumers can switch on. The categories in this iteration:

- `KindParseFailure` — parser returned errors.
- `KindElaborationFailure` — pipeline elaborator returned errors.
- `KindUnregisteredResolver` — resolver name in the DSL has no Go registration.
- `KindUnregisteredFormatter` — formatter name in the DSL has no Go registration.
- `KindUnsupportedStage` — a handler's effective pipeline includes a stage outside the in-scope envelope (`session`, `csrf`, `limit`, `approve`, `commit`, `emit`, `layout`, `redirect`, `log`, `measure`).
- `KindUndeclaredRouteParameter` — a resolver argument `:name` does not resolve to a parameter declared in the handler's route pattern.
- `KindRouteAmbiguity` — two handlers declared on the same method-and-path.
- `KindMissingEnvVar` — a required environment variable did not resolve. The skeleton iteration has no required env vars (both `PORT` and `WRIT_ENV` have defaults), so this kind is reserved for later features but exists in the API surface for symmetry.

Each error carries the source span(s) of the offending construct and a human-readable message. `Load()` returns a single aggregate error that exposes the underlying list in source order; consumers can iterate by kind without parsing message strings.

## Routing

Each elaborated handler contributes a route to the dispatcher's routing table. The routing table is built once at startup:

- A route is matched on HTTP method and URL path. Method match is exact (case-sensitive, matching the parser's case rule from spec 001).
- Path matching uses the parsed `*ast.RoutePattern`: literal segments match by exact equality; parameter segments (`:name`) match any non-empty segment value and bind it to the parameter name; trailing wildcards are not allowed in handler routes (spec 001 reserves wildcards for `group` and `errors` patterns).
- A request that matches no handler returns a `404 Not Found` with a generic body.
- A request that matches a path but not a method returns a `405 Method Not Allowed` with a generic body. The `Allow` header lists the methods that *do* match the path.
- Path matching is strict on trailing slashes: `/users` and `/users/` are distinct, and the runtime performs no normalization or redirect. The DSL parser (spec 001) already rejects trailing slashes in handler routes, so a handler can never *declare* `/users/`; an incoming `/users/` request simply produces a 404.
- Route ambiguity (two handlers declared on the same method-and-path) is a startup error, reported with both source spans.

## Request Lifecycle

For every accepted request:

1. The dispatcher matches the request against the routing table. A miss produces 404 or 405 per the routing rules above.
2. The runtime extracts route parameters into the request's parameter table.
3. For each `resolve` step in the handler's effective pipeline, in canonical order: build the resolver's argument list from the parameter table, invoke the registered resolver, store the result under the step's variable name. If the resolver returns an error, short-circuit with a generic 500.
4. Invoke the registered formatter for the handler's `format` step, passing it the named-result table restricted to the variables listed in the `with` clause. The formatter writes the response.
5. If the formatter has not written a status by the time it returns, the runtime defaults to `200 OK` with the body the formatter wrote (which may be empty).

There is no after-response work in this iteration. `emit` is out of scope.

## Errors and Status Codes

The runtime's error surface in this iteration is intentionally narrow:

- Any non-nil error from a resolver short-circuits the request. The runtime writes `500 Internal Server Error` with a generic body and `Content-Type: text/plain; charset=utf-8` (the runtime owns these bytes). The error's content is not exposed to the client. The `errors` block from the DSL is out of scope, so there is no per-route customization.
- A formatter that returns an error before writing a status produces the same 500.
- A formatter that returns an error *after* writing a status logs the error (out of scope; for this iteration, the runtime accepts that the response was partially written and moves on).
- Successful resolve + format produces `200 OK` unless the formatter writes a different status.
- Routing failures produce 404 or 405 as above.

Status-code inference based on commit, redirect, approve, limit, or CSRF is out of scope (those stages are out of scope).

## HTTP Boundary

The runtime is a `net/http`-compatible handler. This means:

- Standard Go middleware (CORS, request ID, panic recovery, gzip) wraps the runtime's handler from the outside. The runtime does no panic recovery itself — an unrecovered panic in a resolver or formatter propagates per `net/http` semantics.
- The runtime does not consume the request body unless an in-scope feature requires it. (None do; `body` parsing is out of scope.) Outside middleware is free to wrap the body.
- The runtime imposes no per-request timeout. `Run` constructs a stock `http.Server` with no timeout configuration; users that need timeouts use `Load` + their own `http.Server{ReadTimeout: ..., WriteTimeout: ..., IdleTimeout: ...}` and call `Server.Serve` themselves. Per-request context cancellation on client disconnect comes from `net/http` for free; resolvers that respect `ctx.Done()` get the right behavior without any runtime support.
- `Handler()` is safe to call once after `Load()` and is not safe to mutate registrations after that point. Adding a registration after `Load()` is a programming error and produces a runtime panic with a clear message.

## Acceptance Criteria

### Construction and Registration

- [ ] A new runtime instance is created with no registrations and no loaded `.writ` file. Calling `Handler()` before loading is a runtime panic with a message naming the missing load step.
- [ ] Registering a resolver under a name that has already been registered is an error returned from the registration call.
- [ ] Registering a formatter under a name that has already been registered is an error returned from the registration call.
- [ ] Registering a resolver or formatter after `Load()` panics with a clear message naming the registration call.

### Loading and Validation

- [ ] Loading a `.writ` file with parse errors returns a single error whose detail lists every parser error in source order; the runtime is left unloaded.
- [ ] Loading a `.writ` file that elaborates with errors (per spec 002) returns a single error whose detail lists every elaboration error in source order; the runtime is left unloaded.
- [ ] Loading a `.writ` file that references an unregistered resolver name fails with an error naming the resolver and the source span of the referencing handler. Multiple unregistered references are all reported in a single load call.
- [ ] Loading a `.writ` file that references an unregistered formatter name fails with an error naming the formatter and the source span of the referencing handler.
- [ ] Loading a `.writ` file with a handler whose effective pipeline contains an out-of-scope stage (`session`, `csrf`, `limit`, `approve`, `commit`, `emit`, `layout`, `redirect`, `log`, `measure`) fails with an error naming the stage and its source span.
- [ ] Loading a `.writ` file where a resolver argument references an undeclared route parameter fails with an error naming the parameter and the source span of the argument.
- [ ] Loading a `.writ` file with two handlers on the same method and path fails with both source spans.
- [ ] Loading the same path twice (idempotent re-load) returns an error rather than silently replacing state.
- [ ] The aggregate startup error returned by `Load()` exposes its underlying entries with stable typed `Kind` values; a consumer can iterate the entries and switch on `Kind` without parsing message strings.

### Routing

- [ ] A `GET /users/:id` handler matches `GET /users/42` with `id` bound to `"42"`.
- [ ] A `GET /users/:id` handler does not match `POST /users/42`; the response is `405 Method Not Allowed` with `Allow: GET`.
- [ ] A request whose path is not declared by any handler returns `404 Not Found`.
- [ ] When two handlers are declared on the same path with different methods (e.g., `GET /users/:id` and `DELETE /users/:id`), each method dispatches to its handler and the `Allow` header on a 405 lists every declared method, comma-and-space-separated, in alphabetical order (`DELETE, GET`).
- [ ] Path matching is exact: `/users/42` does not match a handler declared on `/users/:id/posts`.
- [ ] Trailing slashes are not normalized: a request to `/users/` does not match a handler declared on `/users` and returns 404 (or 405 if some other method matches `/users/`).

### Resolve

- [ ] When a handler declares `resolve user = db.users(:id)` and the registered `db.users` returns `{name: "Alice"}`, the formatter receives `user` mapped to that value.
- [ ] Multiple resolve steps execute in declaration order; each result is keyed by its variable name in the formatter's input.
- [ ] When a resolver returns a non-nil error, the response status is `500` and no formatter is invoked.
- [ ] A handler with zero resolve steps invokes the formatter with an empty named-result table.

### Format

- [ ] When the format line is `format users.list with users` and the formatter writes `200` with a JSON body, the client receives that exact status and body.
- [ ] A formatter that writes a status other than `200` (e.g., `201`) — the response carries that status.
- [ ] A formatter that writes only a body without setting a status produces `200 OK` with the written body.
- [ ] A formatter that returns an error before writing produces `500 Internal Server Error` with a generic body.
- [ ] A formatter receives only the named results listed in the `with` clause; values resolved but not listed are not visible to it.

### Lifecycle and HTTP Boundary

- [ ] `Handler()` returns a value satisfying `http.Handler`; wrapping it in a third-party middleware chain works without modification.
- [ ] The convenience listener method binds on the port from `PORT` (default `8080`) and blocks until interrupted.
- [ ] An unrecovered panic in a resolver or formatter propagates per `net/http` semantics (the standard library writes a 500 by default and the connection is closed). The runtime does not install its own panic recovery.
- [ ] The runtime never reads the `.writ` file after `Load()` returns; deleting or modifying the file at runtime has no effect on serving.
- [ ] The runtime imposes no per-request timeout; a caller's `http.Server{ReadTimeout, WriteTimeout, IdleTimeout}` configuration applies unmodified.
- [ ] Calling `Load()` while another `Load()` is in progress on the same instance panics with a message identifying the concurrent-load violation.

### Determinism and Isolation

- [ ] Two requests to the same handler do not share state; each request gets its own parameter table and named-result table.
- [ ] `Load()` is deterministic: loading the same source file twice (in two separate processes or two separate runtime instances) produces equal routing tables.

## Open Questions

*All open questions resolved.*

## Resolved Questions

- **Loader API surface** — Expose all three: `Load(path) error`, `Handler() http.Handler`, and `Run(path) error`. `Run` is sugar that calls `Load` then binds the listener and serves. Rationale: `Load` + `Handler` is the testable seam — `httptest.NewServer` and middleware-composition tests need a handler without a bound port. `Run` is the ergonomic path the README example uses (`w.Run("app.writ")`); removing it would force every `main.go` to write the three-line dance manually. Idiomatic Go HTTP frameworks (`chi`, `echo`, `gin`) all expose both shapes for the same reason. The cost is one method, and `Run` literally is `Load` + `ListenAndServe(Handler())` — no semantic divergence.
- **Resolver argument shape** — `func(ctx context.Context, params Params) (any, error)`, where `Params` is a small accessor type. `params.String(name)` returns the route parameter value bound by the DSL `:name` argument; missing keys return `""` rather than panicking (a missing key at runtime can only happen via test harness misuse, since startup validation already checks parameter completeness). Accessor surface in this iteration is just `String`; richer accessors (`Int`, typed bodies, field references) layer on in later features without changing the signature. Rationale: map-typed exposes a public collection the runtime can't evolve; variadic positional drops the parameter name the README's resolver examples rely on (`params.String("team_id")`). The `Params` shape lets codegen later generate typed wrappers around the same signature rather than replacing it.
- **Formatter named-result type** — `func(ctx context.Context, w http.ResponseWriter, data Results) error`, where `Results` is a small accessor type symmetric with `Params`. `data.Get(name)` returns the raw `any` resolver result for a name listed in the format line's `with` clause; `data.Has(name)` reports presence. Iteration-by-name is not exposed in this iteration. Rationale: symmetry with `Params` keeps the runtime surface coherent. A `map[string]any` exposes the underlying collection and forces later features (typed access, codegen wrappers) into either parallel APIs or breaking changes. Restricting `Results` to the `with` clause is the runtime-side enforcement of the spec's "formatter receives only listed names" rule. Skipping iteration shrinks the surface so codegen can later generate typed accessors (`results.User()`) without competing with a generic iterator.
- **Listener convenience method** — `w.Run(path string) error` is the single signature. It performs `Load(path)`, then binds on the port from `PORT` (default `8080`) and serves until the process is interrupted. `Run` does not accept an address argument. Users that need a non-`PORT` address use `Load(path)` + `http.ListenAndServe(addr, w.Handler())` directly — two lines of standard `net/http` code. Rationale: the README example writes `w.Run("app.writ")` with no address argument; honoring that signature is what makes the spec match the documented surface. `system.md` already defines `PORT` as the reserved env var with a default; threading an address through `Run` duplicates that mechanism. A separate `ListenAndServe()` would still require a prior `Load(path)`, which defeats the point of the convenience.
- **`.env` file loading and `WRIT_ENV` mode behavior** — Defer both. The skeleton runtime reads `PORT` and `WRIT_ENV` directly from the process environment via `os.Getenv` and ignores `.env` files. `WRIT_ENV` is parsed at startup but does not gate any in-scope behavior in this iteration. Tracked in `specs/inbox.md` under the **Configuration** item, which already bundles `.env` loading, `WRIT_ENV` mode behavior, `w.Config(name, env_var)`, and fail-fast required-var validation as one future feature. Rationale: `.env` parsing is a third-party-library + precedence-rule + file-discovery decision worth its own spec; `WRIT_ENV`-driven behavior changes (verbose logs, detailed errors, hot reload) are themselves out of scope, so adding the gate now would be dead code. Adding `.env` loading later is purely additive — it expands the set of values the runtime sees without changing how it consumes them.
- **Default `Content-Type` on success responses** — The runtime never touches response headers on the success path. A formatter that does not set `Content-Type` gets the standard `net/http` behavior: `http.ResponseWriter` falls back to `http.DetectContentType` on the first write. For runtime-owned responses (the generic 500 produced when a resolver or formatter fails), the runtime sets `Content-Type: text/plain; charset=utf-8` because it owns those bytes. Rationale: extending "the formatter writes the response" to include headers keeps the rule one sentence long; picking any default mime type would silently mislabel responses from formatters that intend something else (CSV, plain text, HTML when later features add templates). The 500 carve-out is necessary because the runtime is the writer for those responses, and sniffing a short generic message is unreliable.
- **`Allow` header construction on 405** — Methods are written verbatim (uppercase per spec 001), comma-and-space-separated (`", "`), sorted alphabetically by byte-wise comparison. The order is stable across `Load()` calls because the routing table is deterministic per spec 002's determinism criterion. For a path matched by `GET`, `DELETE`, and `PUT`, the `Allow` header on a 405 is `DELETE, GET, PUT`. Rationale: RFC 9110 §10.2.1 doesn't require an order, but sorted output keeps test snapshots, cache keys, and screenshots stable across runs. Source order would tie the header to `.writ` declaration order, creating flaky tests when files are reorganized. Verbatim (no folding) honors the parser's uppercase-method rule.
- **Trailing slash policy** — Strict matching. `/users` and `/users/` are distinct paths; the runtime performs no normalization or auto-redirect. The DSL parser (spec 001) already rejects trailing slashes in handler routes, so a handler can never *declare* `/users/`; an incoming `/users/` request produces a 404 (or 405 if some other method matches `/users/`). Rationale: silently treating `/users/` as `/users` would create runtime behavior the DSL grammar forbids — a confusing inconsistency. Auto-redirect (`301` from `/users/` to `/users`) is a legitimate production pattern but a routing-policy decision worth its own spec; the skeleton stays simple and adding a redirect later is purely additive. Strict matching also matches `chi`, `httprouter`, and most Go routers' defaults.
- **Test-helper surface** — Defer. The skeleton runtime ships only what's already exposed: `Load(path)` + `Handler() http.Handler`. The standard testing pattern is `w := writ.New(); w.Resolver(...); w.Load("test.writ"); srv := httptest.NewServer(w.Handler())` — idiomatic `net/http` testing with no Writ-specific helpers. Tracked in `specs/inbox.md` under the **Testing DSL (.test.writ)** item, which will own dedicated helpers when a real consumer (the `.test.writ` runner) drives their shape. Rationale: the constitution forbids designing for hypothetical future requirements. `writ.TestPipeline` is a thin wrapper over the seam already exposed by `Load` + `Handler`; designing it without a real consumer would be speculative API. Keeping the skeleton's public surface to five methods (`Resolver`, `Formatter`, `Load`, `Handler`, `Run`) makes it trivial to audit and document.
- **Structured startup error type** — Yes. The runtime defines a typed error with an exported `Kind` field consumers can switch on, matching the convention `parser.Error` (spec 001) and `pipeline.Error` (spec 002) already establish. Kinds in this iteration: `KindParseFailure`, `KindElaborationFailure`, `KindUnregisteredResolver`, `KindUnregisteredFormatter`, `KindUnsupportedStage`, `KindUndeclaredRouteParameter`, `KindRouteAmbiguity`, `KindMissingEnvVar`. `Load()` returns a single aggregate error whose entries are iterable in source order. Rationale: continuing the parser/elaborator pattern is convention-over-configuration for the framework's error surface and matches the user's preference for extending existing mechanisms uniformly. An LSP, future `writ doctor` verb, or test asserting "this misconfiguration produces *that* category of error" all benefit from stable codes — without them, every consumer parses the message string, which becomes load-bearing.
- **Idempotent registration for overrides** — Replacement is always an error. Tests use a fresh `writ.New()` instance with test doubles registered up front (the standard Go HTTP-testing idiom). The acceptance criteria already encode this; this resolution confirms it against the test-doubles use case. Rationale: allowing replacement creates an order-dependent registration model where the *last* `w.Resolver("db.users", ...)` wins — silent global state that depends on call order. A typo at the registration site would be silently shadowed by the last one, exactly the kind of bug a startup error catches. `writ.New()` is cheap; tests that need different doubles per case build a new instance per case. If a real consumer (e.g., the `.test.writ` runner) ever needs in-place swap, that consumer can request an explicit `ReplaceResolver()` whose semantics are spelled out — safer than baking a "last write wins" rule into the basic call.
- **Handler timeout** — The runtime imposes no per-request timeout. `Run` constructs a stock `http.Server` with no timeout configuration; users that need timeouts use `Load` + their own `http.Server{ReadTimeout, WriteTimeout, IdleTimeout, ReadHeaderTimeout}`. Per-request context cancellation on client disconnect comes from `net/http` for free. Rationale: timeouts are a deployment-policy decision, not a runtime decision — different deployments (proxy in front, no proxy, long uploads, longpoll) want different values. `net/http`'s timeout knobs are the canonical Go controls; layering a Writ-specific timeout on top would be redundant and would conflict with timeouts set at the `http.Server` level. Defaulting to no timeout also matches `http.ListenAndServe`'s own default — zero surprise.
- **Concurrent `Load()` calls** — Programming error. The runtime panics if `Load()` is called while another `Load()` is in progress on the same instance, with a clear message naming the violation. After a successful load, a second `Load()` call returns the "already loaded" error (the existing same-path-twice criterion). Rationale: `Load` is a startup operation called once from `main()` before any goroutine is launched; there is no real use case for concurrent loading. Adding a mutex around all setup state would be dead-weight for a once-per-process operation. A panic at the second call with a clear message helps developers find the bug; silent serialization or "first wins" hides it. Same lifecycle discipline as the existing post-`Load()` registration rule.
