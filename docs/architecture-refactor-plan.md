# CC-Connect Architecture Refactor Plan

## Goal

Improve cohesion, reduce coupling, and make behavior easier to test without changing product semantics.

The work must move in small, independently verifiable steps. Each code refactor task must preserve behavior and must finish with both local tests and a real Feishu e2e test.

## Non-Negotiable Rules

1. Do not restart or modify the production daemon.
2. Use the isolated test config only: `~/.cc-connect-test/config.toml`.
3. Every code refactor task must run:
   - targeted local tests for the changed package or behavior;
   - broader local tests for the touched subsystem;
   - real Feishu e2e: `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`.
4. The real e2e test must use the fixed test group encoded in `tools/e2e/real_feishu_e2e.sh` unless a task explicitly requires another target.
5. Do not combine unrelated refactors in one task. One task should touch one responsibility boundary.
6. If e2e fails after a refactor, stop. Diagnose and fix that task before starting the next task.
7. Keep each task as a separate commit unless the user explicitly asks otherwise.

## Current Architecture Findings

The intended architecture is sound:

```text
cmd/cc-connect  -> config, core, agent/*, platform/feishu
agent/*         -> core
platform/feishu -> core
core            -> standard library and shared dependencies only
```

The main issue is responsibility concentration:

- `core/engine.go` is about 16k lines and mixes lifecycle, message routing, command handling, scheduler execution, send/reply output, card rendering, workspace resolution, relay, and session orchestration.
- `platform/feishu/feishu.go` is about 6k lines and mixes websocket/webhook startup, event parsing, session-key policy, Feishu API calls, rich cards, attachment upload/download, mention resolution, and batching.
- Existing tests are useful, but many tests still need large `Engine` or `Platform` objects because pure logic is embedded in I/O-heavy types.

## Execution Model

Default execution is serial. Parallel work is allowed only when write sets do not overlap and both tasks have independent test/e2e gates.

Task format:

1. Identify one responsibility boundary.
2. Move or extract logic without changing behavior.
3. Add or move focused tests when useful.
4. Run local tests.
5. Run real Feishu e2e.
6. Commit that task.
7. Update this plan with task status, commit id, and validation evidence.
8. Send a new root message in the current group, not a reply in the current topic, and @ the cc-connect development bot to start the next task.

The handoff message must include the next task title, the target files, and the required test gate. Do not use a topic reply for this handoff; it must create a separate discussion thread for the next task.

## Task 1: Engine Lifecycle Split

Status: completed in `61f4a52 refactor: split restart notification flow`.

Scope:

- Move restart notification and other lifecycle-only helpers out of `core/engine.go`.
- Keep package `core`; do not introduce a new package.
- Do not change public behavior.

Candidate files:

- `core/restart_notify.go`
- optional later files: `core/engine_lifecycle.go`

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'TestRestartNotify|TestRestart'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Completed validation:

- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`
- `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`
- e2e message: `om_x100b6cf87c842088b48c1d3b2973b66`

Parallelism:

- Serial with other `core/engine.go` refactors.
- Can run in parallel only with isolated `platform/feishu` pure parsing tasks.

## Task 2: Engine Outbound Delivery Boundary

Status: completed in `1a1d1b1 refactor: split engine outbound delivery`.

Scope:

- Isolate send/reply/rate-limit/card-output helpers from `core/engine.go`.
- Keep behavior in `core`; avoid a new public abstraction unless needed.
- Target methods around `sendWithError`, `replyWithError`, `sendWithCard`, `replyWithCard`, and outgoing render helpers.

Goal:

- Make outbound delivery behavior testable without entering message-processing paths.
- Reduce coupling between command handlers and platform-specific rendering capabilities.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Send|Test.*Reply|Test.*Card|TestOutgoing|TestCUJ'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Completed validation:

- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Send|Test.*Reply|Test.*Card|TestOutgoing|TestCUJ'`
- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`
- `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`
- e2e message: `om_x100b6cf95cab7098b163ba7e798b8a6`

Parallelism:

- Serial with other `core/engine.go` refactors.
- Do not run in parallel with command/card refactors.

## Task 3: Command Dispatch Boundary

Status: completed in `refactor: split command dispatch boundary`.

Scope:

- Split command parsing and dispatch table logic from command implementations where possible.
- Keep command behavior and user-facing strings unchanged.
- Avoid migrating all commands at once.

First slice:

- Extract dispatch metadata and alias/disabled-command checks.
- Leave command implementations in place until dispatch tests are stable.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Command|Test.*Alias|TestCUJ'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Completed validation:

- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Command|Test.*Alias|TestCUJ'`
- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`
- `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`
- e2e message: `om_x100b6cf98b57dcacb13f452ec123336`

Parallelism:

- Serial with outbound delivery and card refactors.

## Task 4: Scheduler Execution Boundary

Status: completed in `refactor: split scheduler execution helpers`.

Scope:

- Separate cron/timer execution orchestration from interactive user-message handling.
- Keep scheduler storage and public commands unchanged.
- Focus on execution helpers before command UI.

Goal:

- Test cron/timer execution with narrow fakes instead of full `Engine` setup.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Cron|Test.*Timer|TestCUJ'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Completed validation:

- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run 'Test.*Cron|Test.*Timer|TestCUJ'`
- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`
- `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`
- e2e message: `om_x100b6cf9a58bf8a8b21d85d26a7ee01`

Parallelism:

- Serial with command dispatch work.
- Can run after outbound delivery is stable.

## Task 5: Feishu Event Parsing Boundary

Status: completed in `refactor: split feishu event parser`.

Scope:

- Extract pure conversion from Feishu SDK event objects to `core.Message` inputs.
- Keep websocket/webhook startup and API clients in `platform/feishu`.
- Do not alter session key semantics in the same task.

Goal:

- Test mention filtering, content extraction, sender metadata, and attachment routing without live SDK clients.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu -run 'TestOnMessage|Test.*Mention|Test.*Attachment|Test.*Thread'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Completed validation:

- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu -run 'TestOnMessage|Test.*Mention|Test.*Attachment|Test.*Thread'`
- `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu`
- `CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e`
- e2e message: `om_x100b6cfa2443d89cb04dbf649ef0718`

Parallelism:

- Can run in parallel with `core` lifecycle work.
- Do not run in parallel with Feishu session-key or send API refactors.

## Task 6: Feishu Session-Key Policy Boundary

Scope:

- Extract session-key construction and reply-target decisions from `platform/feishu/feishu.go`.
- Keep existing keys stable.
- Cover group, topic/thread, P2P, relay visibility, and card-action cases.

Goal:

- Make channel/thread behavior testable as policy logic instead of event/API side effects.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu -run 'Test.*SessionKey|Test.*Thread|TestRelayGroupVisibility'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Parallelism:

- Serial with Feishu event parsing and Feishu send API work.

## Task 7: Feishu Send API Boundary

Scope:

- Isolate Feishu create/reply/patch/upload/download API operations behind narrow internal helpers.
- Keep `core.Platform` interface unchanged.
- Do not change message formatting in this task.

Goal:

- Make retry/token-refresh behavior and reply-vs-create decisions easier to test independently.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu -run 'Test.*Retry|Test.*Reply|Test.*Send|Test.*Upload|Test.*Download'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Parallelism:

- Serial with other Feishu I/O refactors.

## Task 8: Feishu Rich Card Rendering Boundary

Scope:

- Move pure rich-card rendering helpers out of `platform/feishu/feishu.go` into focused files.
- Keep `platform/feishu/card.go` v1 card behavior stable.
- Do not mix card rendering with send/update API refactors.

Goal:

- Reduce `feishu.go` size and make card snapshots easier to update.

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu -run 'Test.*Card|Test.*Markdown|Test.*Rich'
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu
CC_REAL_FEISHU_E2E=1 make test-real-feishu-e2e
```

Parallelism:

- Can run after Feishu send API boundaries are stable.
- Do not run in parallel with card send/update changes.

## Stop Conditions

Stop and report before continuing if any of these happen:

- e2e fails after local tests pass;
- a task requires changing `core.Platform`, `core.Agent`, or `core.AgentSession`;
- a task requires touching production config or production daemon state;
- a task needs broad rewrites across `core`, `platform/feishu`, and `agent/*` in the same commit;
- Feishu topic-group behavior becomes part of the acceptance target.

## Commit Policy

Each implementation task should produce:

1. one small behavior-preserving refactor commit;
2. a final note with local test commands and real e2e evidence;
3. no unrelated formatting or generated-file churn.

The plan document itself is committed separately before implementation work continues.
