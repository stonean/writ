---
description: Display an overview of the pipeline and its slash commands.
---

# Help

Display an overview of the pipeline and how to use its slash commands.

## Instructions

Print the following guide exactly (do not scan files or run commands):

---

## writ — Spec-Driven Development Pipeline

writ is a set of slash commands that guide features from idea to implementation through a structured pipeline.

### Pipeline States

```text
draft → clarified → planned → in-progress → done
```

Two back-edges keep the lifecycle honest:

- `/writ:ask` reverts a `clarified`, `planned`, or `in-progress` spec to `draft` when a new open question surfaces — `draft` is the only status that tolerates open questions. The next `/writ:clarify` resolves the question and the spec advances forward again.
- `/writ:elaborate` reverts a `done` spec to `in-progress` when a new scenario is added — the scenario captures the change, the spec evolves with it.

Each feature lives in `specs/NNN-feature-name/` and progresses through these states by running the corresponding command.

### Commands

#### Pipeline (advance state)

| Command | Pipeline Gate | Description |
| --- | --- | --- |
| `/writ:specify` | → draft | Create a new feature spec. Pass a short description, e.g. `/writ:specify webhook delivery`. |
| `/writ:clarify` | draft → clarified | Resolve open questions in the spec. Works on the session target or pass a feature identifier. Has a recovery path for hand-edited specs that arrive non-`draft` with open questions in the body. |
| `/writ:plan` | clarified → planned | Generate `plan.md` and `tasks.md` with implementation details. |
| `/writ:implement` | planned → in-progress → done | Execute the tasks for the targeted feature. |
| `/writ:validate` | — | Audit artifacts for consistency, completeness, and cross-spec alignment. |

#### Elaborate (add precision)

| Command | Description |
| --- | --- |
| `/writ:ask` | Append an open question to the targeted spec or scenario for resolution during clarify. On a `clarified`, `planned`, or `in-progress` spec, also reverts status to `draft` (the back-edge); refuses on `done` specs and points at `/writ:elaborate` instead. |
| `/writ:elaborate` | Create a scenario file for the targeted feature. Walks the bug decision tree, creates the file in `scenarios/`, and appends a task to `tasks.md`. |

#### Brownfield (absorb existing reality)

| Command | Description |
| --- | --- |
| `/writ:capture` | Initialize a skeleton spec from a freeform description of an existing feature. |
| `/writ:log` | Record a raw item to `specs/inbox.md` for later grooming. |
| `/writ:groom` | Walk `specs/inbox.md` and route each item to its proper spec or scenario via the bug decision tree. |

#### Orient

| Command | Description |
| --- | --- |
| `/writ:target` | Set the working feature (or feature/scenario) for the session. Pass a number (`001`), partial name (`api-versioning`), or full directory name. |
| `/writ:status` | Dashboard showing every feature's progress, dependencies, artifacts, and blockers. |
| `/writ:help` | This overview. |

#### Bootstrap (one-time per project)

| Command | Description |
| --- | --- |
| `/govern` | Adopt or update governance in an existing project. Installed in your agent's command directory; no project namespace. |
| `/writ:configure` | Configure `.claude/settings.local.json` so commands run without manual approval prompts. |
| `/writ:spawn` | Spawn a new project from this one — copies specs, commands, and configuration. |

### Typical Session

```text
/writ:configure                 # first time only
/writ:status                    # see where everything stands
/writ:target 000                # pick a feature to work on
/writ:clarify                   # resolve open questions
/writ:plan                      # generate implementation plan
/writ:implement                 # write the code
```

### Key Concepts

- **Session target** — The feature you're currently working on, stored in `.claude/writ-session.json`. Most commands operate on the target by default.
- **Dependencies** — Features declare dependencies in their spec. A feature is blocked until its dependencies reach `clarified` or later.
- **Artifacts** — Each feature directory can contain `spec.md`, `plan.md`, `tasks.md`, `data-model.md`, and a `scenarios/` subdirectory.
- **Scenarios** — A scenario is a spec at a lower level of abstraction. Scenarios live in `specs/NNN-feature/scenarios/slug.md` and capture bugs, edge cases, and detailed behavior. Each scenario gets a linked task in `tasks.md`.
- **Bug decision tree** — When a bug is reported: (1) no spec → write the spec first, (2) spec is ambiguous → fix the spec, (3) spec is clear → add a scenario.
- **Inbox** — `specs/inbox.md` is a temporary inbox for known issues. Items are recorded with `/writ:log` and groomed into specs or scenarios with `/writ:groom`.
- **Finish before moving on** — Prefer completing a feature through the full pipeline before starting the next. Depth-first keeps context focused.

---
