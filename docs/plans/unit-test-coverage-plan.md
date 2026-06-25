# Unit Test Coverage Improvement Plan

## Goals

- Reach `test:business >= 1.20:1` by Go source lines.
- Reach business-code statement coverage `>= 65%`.
- Keep `go test ./...` stable before increasing coverage.
- Improve coverage by module boundaries, not by duplicated fixtures or empty assertions.

Current baseline from `feat/feishu-only`:

| Metric | Value |
| --- | ---: |
| Test Go lines (`*_test.go`) | 71,899 |
| Business Go lines (`*.go`, excluding `*_test.go`, `tests/**`, `web/**`) | 83,722 |
| Current `test:business` | `0.86:1` |
| Target test lines at `1.20:1` | 100,466 |
| Test-line gap | 28,567 |
| Business-code coverage | 50.5% |

Baseline package coverage:

| Package | Current | Target |
| --- | ---: | ---: |
| `cmd/cc-connect` | 14.8% | 40%+ |
| `agent/traex` | 13.4% | 60%+ |
| low-coverage agent adapters | 13%-41% | 50%+ |
| `core` | 60.8% | 68%-72% |
| `platform/feishu` | 55.0% | 65%+ |
| `config` | 66.5% | 75%+ |
| `daemon` | 74.0% | 80%+ |

## Non-Goals

- Do not add tests that only increase line count without assertions.
- Do not use snapshot-heavy tests for behavior that can be asserted directly.
- Do not make unit tests depend on real Feishu/Lark network calls.
- Do not start, stop, or mutate the production cc-connect daemon.
- Do not use production `~/.cc-connect/config.toml`, production logs, or production data dirs in tests.

## Shared Test Rules

- Use package-level `go test` as the normal validation unit.
- Use `GOCACHE=/private/tmp/cc-connect-go-cache` in the local macOS environment.
- Replace fixed sleeps with condition-based wait helpers where possible.
- Prefer fake process, fake platform, fake HTTP client, and in-memory stores over real external services.
- Keep shared helpers in the package that needs them unless multiple packages already share the same test boundary.
- Every bug fix gets a focused regression test.

## Status Values

Use the `Status` column to track each task and work package.

| Status | Meaning |
| --- | --- |
| `todo` | Not started. |
| `in_progress` | Being implemented. |
| `blocked` | Waiting on dependency, decision, or prerequisite fix. |
| `review` | Implementation is ready for review. |
| `done` | Merged or accepted, validation passed. |

## Serial Prerequisites

These tasks should be completed before broad parallel coverage work.

| Task | Status | Scope | Required Outcome |
| --- | --- | --- | --- |
| L0-1 | `in_progress` | `agent/codex` | Stabilize `TestGetModelAndReasoningEffort_FromRuntimeConfigWhenUnset` under full-suite execution. |
| L0-2 | `todo` | Test helpers | Add or standardize condition-based wait helpers for files, channels, and async state changes. |
| L0-3 | `todo` | Fake processes | Standardize fake CLI/process helpers for stdin/stdout/RPC interactions. |
| L0-4 | `todo` | Coverage scripts | Add repeatable local commands for line ratio, package coverage, and low-coverage functions. |

Validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./...
GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/codex -count=20
```

## Module 1: Agent Adapter Layer

Agent packages are mostly independent and can be developed in parallel after L0.

Primary packages:

- `agent/traex`
- `agent/antigravity`
- `agent/cursor`
- `agent/gemini`
- `agent/kimi`
- `agent/acp`
- `agent/qoder`
- `agent/tmux`

Secondary packages:

- `agent/codex`
- `agent/claudecode`
- `agent/opencode`
- `agent/pi`
- `agent/copilot`
- `agent/iflow`

### Agent Test Matrix

| Task | Status | Coverage Target |
| --- | --- | --- |
| A-1 Basic attributes | `todo` | `Name`, `CLIBinaryName`, `GetWorkDir`, `SetWorkDir`, default model/mode/reasoning effort. |
| A-2 Config state | `todo` | `SetModel`, `SetMode`, provider switching, env merge, allowed tools, memory dirs, skill dirs, command dirs. |
| A-3 Start arguments | `todo` | `StartSession` command args, workdir, env, resume/session id, permission mode. |
| A-4 Session I/O | `todo` | `Send`, `Events`, `Alive`, `Close`, `CancelTurn`, early process exit, stderr handling. |
| A-5 Event parsing | `todo` | Raw agent output to `core.AgentEvent`: text, tool call, permission, error, usage, done. |
| A-6 Session management | `todo` | `ListSessions`, `DeleteSession`, history read, session id detection, corrupt metadata, empty dirs. |
| A-7 Error paths | `todo` | Missing CLI, startup failure, malformed JSON, timeout, invalid permission response. |

### Agent Work Packages

| Work Package | Status | Write Scope | Notes |
| --- | --- | --- | --- |
| P1 | `todo` | `agent/traex/*_test.go` | Current branch priority. Bring coverage to `60%+`. |
| P2 | `todo` | `agent/antigravity/*_test.go`, `agent/gemini/*_test.go`, `agent/kimi/*_test.go` | Apply the common agent matrix. |
| P3 | `todo` | `agent/cursor/*_test.go`, `agent/qoder/*_test.go`, `agent/tmux/*_test.go` | Apply the common agent matrix. |
| P4 | `todo` | `agent/acp/*_test.go` | Handle separately because ACP RPC/session protocol is more complex. |
| P5 | `todo` | `agent/codex/*_test.go`, `agent/claudecode/*_test.go`, `agent/opencode/*_test.go` | Fill remaining protocol, model, provider, and event gaps. |

## Module 2: CLI Command Layer

`cmd/cc-connect` has low coverage and many user-facing command branches. Test command parsing and branch behavior, not process entry boilerplate.

Target files:

- `cmd/cc-connect/provider.go`
- `cmd/cc-connect/cron.go`
- `cmd/cc-connect/timer.go`
- `cmd/cc-connect/sessions.go`
- `cmd/cc-connect/send.go`
- `cmd/cc-connect/feishu.go`
- `cmd/cc-connect/daemon.go`
- `cmd/cc-connect/update.go`
- `cmd/cc-connect/config_cmd.go`

| Task | Status | Coverage Target |
| --- | --- | --- |
| C-1 Provider commands | `todo` | list/add/remove/switch, invalid provider, missing args, config write failure. |
| C-2 Cron commands | `todo` | add/edit/delete/list, expression validation, agent/workspace args, duplicate jobs. |
| C-3 Timer commands | `todo` | timer creation, cancel, execution-context parsing, invalid duration. |
| C-4 Session commands | `todo` | list/filter/delete, empty sessions, corrupt metadata, cross-workspace behavior. |
| C-5 Send and relay | `todo` | argument parsing, target session selection, stdin input, error returns. |
| C-6 Feishu commands | `todo` | config validation, event/webhook command branches, missing app config. |
| C-7 Daemon and update | `todo` | start/stop/status branches, update available/fail/skip branches. |
| C-8 Config commands | `todo` | get/set/list, type conversion, unknown keys, write failure. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P6 | `todo` | `cmd/cc-connect/provider*_test.go`, `cmd/cc-connect/config_cmd*_test.go` |
| P7 | `todo` | `cmd/cc-connect/cron*_test.go`, `cmd/cc-connect/timer*_test.go` |
| P8 | `todo` | `cmd/cc-connect/sessions*_test.go`, `cmd/cc-connect/send*_test.go`, `cmd/cc-connect/relay*_test.go` |
| P9 | `todo` | `cmd/cc-connect/daemon*_test.go`, `cmd/cc-connect/update*_test.go`, `cmd/cc-connect/feishu*_test.go` |

## Module 3: Core Orchestration Layer

`core` already has many tests. Add focused tests for state machines, command dispatch, and user-visible behavior.

Target files:

- `core/engine.go`
- `core/engine_commands.go`
- `core/engine_scheduler.go`
- `core/cron.go`
- `core/timer.go`
- `core/api.go`
- `core/webhook.go`
- `core/bridge.go`
- `core/relay.go`
- `core/management.go`
- `core/workspace_binding.go`
- `core/projectstate.go`

| Task | Status | Coverage Target |
| --- | --- | --- |
| E-1 Command dispatch | `todo` | slash commands, unknown commands, invalid args, permission denial, help/i18n. |
| E-2 Session orchestration | `todo` | new session, resume, workspace binding, session id validation, missing agent. |
| E-3 Timer/cron execution | `todo` | scheduled context, failure retry, cancellation, workspace/agent resolution. |
| E-4 Card actions | `todo` | button actions, pending provider add, permission response, invalid action. |
| E-5 Outbound and relay | `todo` | outbound queue, rate limit, thread reply, send failure recovery. |
| E-6 Workspace flow | `todo` | workspace init, binding changes, state persistence, corrupt state recovery. |
| E-7 API and webhook | `todo` | HTTP args, auth, status codes, malformed payloads. |
| E-8 Management | `todo` | static fallback, health/status endpoints, management API boundaries. |

Parallel work packages:

| Work Package | Status | Write Scope | Notes |
| --- | --- | --- | --- |
| P10 | `todo` | `core/cron*_test.go`, `core/timer*_test.go`, `core/engine_scheduler*_test.go` | Low conflict with command/session work. |
| P11 | `todo` | `core/engine*_test.go`, `core/command*_test.go`, `core/session*_test.go` | Keep one owner to avoid large-file conflicts. |
| P12 | `todo` | `core/api*_test.go`, `core/webhook*_test.go`, `core/management*_test.go` | Can run independently. |
| P13 | `todo` | `core/relay*_test.go`, `core/bridge*_test.go`, `core/engine_outbound*_test.go` | Can run independently. |
| P14 | `todo` | `core/workspace_binding*_test.go`, `core/projectstate*_test.go`, `core/workspace_state*_test.go` | Can run independently. |

## Module 4: Feishu Platform Layer

Platform tests should use fake clients and replayed payloads. Unit tests must not call real Feishu/Lark APIs.

Target files:

- `platform/feishu/feishu.go`
- `platform/feishu/event_parser.go`
- `platform/feishu/send_api.go`
- `platform/feishu/rich_card_render.go`
- `platform/feishu/card.go`
- `platform/feishu/session_policy.go`
- `platform/feishu/image_batch.go`
- `platform/feishu/ws_shared.go`
- `platform/feishu/delete_mode_form.go`

| Task | Status | Coverage Target |
| --- | --- | --- |
| F-1 Event parser | `todo` | message receive, mention, image/file, reaction, unknown event, malformed JSON. |
| F-2 Message dispatch | `todo` | group/private chat, bot mention, sender identity, thread/reply ctx, empty messages. |
| F-3 Send API | `todo` | text/card/image send, API error, retry, token refresh, rate limit. |
| F-4 Rich cards | `todo` | progress normalization, tool call rendering, markdown conversion, missing fields. |
| F-5 Reply chain | `todo` | parent/root message, thread visibility, `formatReplyChain` boundaries. |
| F-6 Image batch | `todo` | multiple images, upload failure, partial success, size limit, temp cleanup. |
| F-7 Session policy | `todo` | new session, reused session, group visibility, workspace/session binding. |
| F-8 WebSocket shared | `todo` | reconnect, duplicate event, close/error, concurrent Stop. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P15 | `todo` | `platform/feishu/event_parser*_test.go`, `platform/feishu/session_policy*_test.go` |
| P16 | `todo` | `platform/feishu/send_api*_test.go`, `platform/feishu/token_retry*_test.go`, `platform/feishu/transient_retry*_test.go` |
| P17 | `todo` | `platform/feishu/rich_card*_test.go`, `platform/feishu/card*_test.go`, `platform/feishu/delete_mode_form*_test.go` |
| P18 | `todo` | `platform/feishu/image_batch*_test.go`, `platform/feishu/ws_shared*_test.go`, `platform/feishu/feishu*_test.go` |

## Module 5: Config, Daemon, and Utility Layer

These modules already have better coverage but should be hardened around failure paths and compatibility.

Target packages and files:

- `config`
- `daemon`
- `core/atomicwrite.go`
- `core/redact.go`
- `core/i18n.go`
- `core/ratelimit.go`
- `core/reference_*`
- `core/markdown*`

| Task | Status | Coverage Target |
| --- | --- | --- |
| I-1 Config | `todo` | TOML parsing, defaults, deprecated keys, invalid types, path expansion, secret redaction. |
| I-2 Daemon | `todo` | launchd/systemd/windows manager, pid file, log rotate, permission failure. |
| I-3 Atomic/filesystem | `todo` | atomic write failure, missing dir, concurrent write, permission failure. |
| I-4 Redact/i18n | `todo` | token and app secret masking, missing language key, fallback behavior. |
| I-5 Markdown/reference | `todo` | escaping, code fences, protected links, reference rendering, invalid input. |
| I-6 Rate limit | `todo` | burst, reset, concurrency, zero/negative config. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P19 | `todo` | `config/*_test.go` |
| P20 | `todo` | `daemon/*_test.go` |
| P21 | `todo` | `core/atomicwrite*_test.go`, `core/redact*_test.go`, `core/i18n*_test.go`, `core/ratelimit*_test.go` |
| P22 | `todo` | `core/reference_*_test.go`, `core/markdown*_test.go` |

## Module 6: Integration and Regression Layer

Integration tests should lock cross-module contracts. Do not use this layer to inflate line count.

Target directories:

- `tests/integration`
- `tests/release_local`
- `tests/blackbox`
- `tests/e2e`

| Task | Status | Coverage Target |
| --- | --- | --- |
| R-1 Engine + fake platform | `todo` | message to engine to fake agent to reply, without real Feishu. |
| R-2 Multi-workspace | `todo` | workspace/session isolation, shared state, context switching. |
| R-3 Turn contract | `todo` | agent event order, tool call, permission, done/error. |
| R-4 Media pipeline | `todo` | image/file references, upload failure, text fallback. |
| R-5 Config scenarios | `todo` | platform/agent initialization under different config combinations. |
| R-6 Regression cases | `todo` | one focused test per historical bug. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P23 | `todo` | `tests/release_local/engine_scenarios/**` |
| P24 | `todo` | `tests/release_local/turn_contract/**` |
| P25 | `todo` | `tests/release_local/media_pipeline/**` |
| P26 | `todo` | `tests/release_local/config_scenarios/**` |
| P27 | `todo` | `tests/integration/**`, `tests/blackbox/**`, `tests/e2e/**` |

## Validation Commands

Package-level validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./<package>
GOCACHE=/private/tmp/cc-connect-go-cache go test ./<package> -coverprofile=/tmp/pkg.out
go tool cover -func=/tmp/pkg.out
```

Core engine, command, cron, or timer changes:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run TestCUJ
```

Full validation:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./...
GOCACHE=/private/tmp/cc-connect-go-cache go test ./... -coverprofile=/tmp/cc-connect-coverage.out
go tool cover -func=/tmp/cc-connect-coverage.out
```

Line-ratio check:

```bash
test_lines=$(rg --files -g '*_test.go' | xargs wc -l | tail -1 | awk '{print $1}')
biz_lines=$(rg --files -g '*.go' -g '!*_test.go' -g '!tests/**' -g '!web/**' | xargs wc -l | tail -1 | awk '{print $1}')
awk -v t="$test_lines" -v b="$biz_lines" 'BEGIN { printf "test:biz = %.2f:1\n", t/b }'
```

## Parallelization Rules

- Work packages with disjoint directories can run in parallel.
- `core/engine*_test.go` should have a single owner at a time.
- `agent/acp` should have a single owner because RPC/session helpers are tightly coupled.
- Shared test helper changes should land before dependent package work.
- Do not mix broad helper refactors with module-specific coverage patches.
- Keep each patch scoped to one work package when possible.

## Done Criteria

- `go test ./...` passes reliably.
- `test:business >= 1.20:1`.
- Business-code statement coverage is `>= 65%`.
- Each target package meets its module-level coverage target or has documented exclusions.
- New tests cover assertions for behavior, error handling, or state changes.
- No tests require production credentials, production config, or real Feishu/Lark network access.
