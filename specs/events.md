# Events

Writ's `emit` pipeline stage fires events after the response is sent. Events are non-blocking — the client never waits for an event handler. This document captures the conventions every event must follow and the catalog of events used by features in this project. Catalog entries are added as features publish or subscribe to events.

## Naming Convention

Event names use dot notation: `{resource}.{action}`. Examples:

- `user.created`
- `user.email_changed`
- `order.placed`
- `subscription.cancelled`

The DSL's `emit` line uses the event name verbatim:

```text
emit user.created with user
```

When a NATS or NATS JetStream emitter is registered, the event name maps directly to the NATS subject — `user.created` publishes to the `user.created` subject. The DSL stays the same regardless of the underlying transport.

## Payload Convention

The `with` keyword passes a single named value (a resolve or commit result) as the event payload. Event handlers receive the payload through their `data any` parameter and assert it to the expected type:

```go
func sendWelcomeEmail(ctx context.Context, data any) error {
    user := data.(User)
    return mailer.SendWelcome(user.Email, user.Name)
}
```

The payload type is whatever the named resolve or commit produced — there is no separate envelope wrapper applied by Writ at this stage. (If a wrapper format becomes necessary for cross-system replay or observability, it is captured in a future feature spec rather than baked in here.)

## Listener Registration

Multiple listeners may subscribe to the same event. Each is registered independently:

```go
w.On("user.created", sendWelcomeEmail)
w.On("user.created", notifyAdminSlack)
w.On("user.created", trackAnalytics)
```

In the local-goroutine emitter, every listener runs in its own goroutine. With NATS, every listener becomes a NATS subscription. With NATS JetStream, listeners are durable — events survive process restarts.

`writ.Queue("name")` puts a listener into a NATS queue group so multiple worker instances can load-balance the same event:

```go
worker.On("user.created", sendWelcomeEmail, writ.Queue("email-workers"))
```

## Worker Listeners

Listeners can run in the web process or in a separate worker process. `writ.NewWorker()` constructs a worker that holds only listeners — no HTTP server, no routes — and uses the `writ worker` CLI verb to start.

## Startup Validation

Every event name referenced in an `emit` line must have at least one registered listener at startup. This catches the "I added an emit but forgot the handler" class of bug before any request is served.

## Event Catalog

<!-- Registry of event types. Add entries as features publish or subscribe to events.
     Each entry documents the event type, its payload, and who publishes
     and subscribes to it. Example:

### user.created

Published when a new user is registered.

- **Publisher**: users module (`POST /users` handler)
- **Subscribers**: notifications, audit
- **Payload**: `User` struct (id, name, email)

-->

_No events registered yet._
