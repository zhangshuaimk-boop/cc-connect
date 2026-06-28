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
| L0-1 | `done` | `agent/codex` | Stabilized `TestGetModelAndReasoningEffort_FromRuntimeConfigWhenUnset` under full-suite execution. Validation passed: `go test ./agent/codex -run TestGetModelAndReasoningEffort_FromRuntimeConfigWhenUnset -count=20`, `go test ./agent/codex -count=20`, `go test ./...` with `GOCACHE=/private/tmp/cc-connect-go-cache`. |
| L0-2 | `done` | Test helpers | Standardized condition-based wait helpers in `agent/codex` for file polling, channel closure, and async session state waits. Validation passed: `go test ./agent/codex -count=20`, `go test ./...` with `GOCACHE=/private/tmp/cc-connect-go-cache`. |
| L0-3 | `done` | Fake processes | Standardized `agent/codex` fake CLI/process helpers for stdin/stdout/RPC interactions in `fake_cli_test.go`. Validation passed: `go test ./agent/codex -count=20`, `go test ./...` with `GOCACHE=/private/tmp/cc-connect-go-cache`. |
| L0-4 | `done` | Coverage scripts | Added `tools/coverage/local_stats.sh` for line ratio, package coverage, and low-coverage function reports. Validation passed: `tools/coverage/local_stats.sh line-ratio`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./...`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. |

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
| A-1 Basic attributes | `done` | P1-P5 raised all listed agent package coverage to 51.2%-82.6%, and P5 explicitly added codex matrix coverage for basic attributes. |
| A-2 Config state | `done` | P1-P5 completed the agent adapter packages, and P5 explicitly added codex matrix coverage for config/provider state. |
| A-3 Start arguments | `done` | P1-P5 completed the agent adapter packages, and P5 explicitly added codex coverage for session env/start-session behavior. |
| A-4 Session I/O | `done` | P1-P5 completed the agent adapter packages with package validation, and P5 explicitly added codex coverage for event/helper paths. |
| A-5 Event parsing | `done` | P1-P5 completed the agent adapter packages, and P5 explicitly added codex matrix coverage for event parsing. |
| A-6 Session management | `done` | P1-P5 completed the agent adapter packages, and P5 explicitly added codex coverage for session list/history/delete. |
| A-7 Error paths | `done` | P1-P5 completed the agent adapter packages, and P5 explicitly added codex coverage for error/helper paths. |

### Agent Work Packages

| Work Package | Status | Write Scope | Notes |
| --- | --- | --- | --- |
| P1 | `done` | `agent/traex/*_test.go` | Coverage raised to 82.1%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/traex -coverprofile=/tmp/traex.out`, `go tool cover -func=/tmp/traex.out`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. |
| P2 | `done` | `agent/antigravity/*_test.go`, `agent/gemini/*_test.go`, `agent/kimi/*_test.go` | Coverage raised to antigravity 75.9%, gemini 77.8%, kimi 79.3%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. |
| P3 | `done` | `agent/cursor/*_test.go`, `agent/qoder/*_test.go`, `agent/tmux/*_test.go` | Coverage raised to cursor 65.6%, qoder 82.6%, tmux 56.6%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/cursor ./agent/qoder ./agent/tmux`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./agent/cursor ./agent/qoder ./agent/tmux`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P4 | `done` | `agent/acp/*_test.go` | Coverage raised to 77.0%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/acp`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./agent/acp`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; the second full-suite run passed. |
| P5 | `done` | `agent/codex/*_test.go`, `agent/claudecode/*_test.go`, `agent/opencode/*_test.go` | Coverage raised to codex 60.1%, claudecode 51.2%, opencode 57.5%. Added codex matrix coverage for basic attributes, config/provider state, session env, session list/history/delete, event parsing, and error/helper paths. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/codex ./agent/claudecode ./agent/opencode`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./agent/codex ./agent/claudecode ./agent/opencode`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...`. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |

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
| C-1 Provider commands | `done` | Added command-level tests for provider list/add/remove/import/global commands, invalid provider paths, missing args, and config write failure. Note: `provider switch` is not an implemented CLI subcommand in `provider.go`; current behavior is covered as unknown subcommand without changing production logic. |
| C-2 Cron commands | `done` | Added command-level tests for add/list/edit/exec/delete over a local Unix socket API, positional cron expressions, env project/session context, workspace edit args, invalid expression errors, and duplicate-job errors. |
| C-3 Timer commands | `done` | Added command-level tests for add/list/info/delete over a local Unix socket API, positional delay and env context parsing, absolute `--at` exec requests, invalid duration errors, and cancel errors. |
| C-4 Session commands | `done` | Added command-level tests for sessions list/show/prune, empty sessions, corrupt metadata warnings, index/global-id selection, project-scoped prune behavior, and help/display helpers. |
| C-5 Send and relay | `done` | Added command-level tests for send/relay argument parsing, env and explicit target session selection, stdin input, Unix socket API payloads, usage branches, MIME/helper behavior, and error returns. |
| C-6 Feishu commands | `done` | Added command-level tests for usage/error branches, missing app config, project selection, platform validation, and guidance output without real Feishu/Lark network. |
| C-7 Daemon and update | `done` | Added daemon parse/log/status/start/stop branches with temp files and fake launchctl, plus update release fetch fallback, pre-release/Gitee skip, download failure/success, archive extraction, asset naming, copy, and replace helpers. |
| C-8 Config commands | `done` | Added command-level tests for config example/path/format/fmt, unknown subcommands, missing file, invalid TOML, and write failure. Note: `config get/set/list` are not implemented CLI subcommands in `config_cmd.go`; current behavior is covered as unknown subcommand without changing production logic. |

Parallel work packages:

| Work Package | Status | Write Scope | Notes |
| --- | --- | --- | --- |
| P6 | `done` | `cmd/cc-connect/provider*_test.go`, `cmd/cc-connect/config_cmd*_test.go` | Coverage raised for `cmd/cc-connect` from 14.8% to 22.0%; `config_cmd.go` reached 100%, provider add/list/remove/import/global helpers covered. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient `agent/antigravity` fake CLI timeout; immediate package rerun and second full-suite run passed. |
| P7 | `done` | `cmd/cc-connect/cron*_test.go`, `cmd/cc-connect/timer*_test.go` | Added local Unix socket command API tests for cron and timer commands without real Feishu/Lark network or daemon use. Coverage raised for `cmd/cc-connect` from 22.0% to 31.1%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p7.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run and one full coverage run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P8 | `done` | `cmd/cc-connect/sessions*_test.go`, `cmd/cc-connect/send*_test.go`, `cmd/cc-connect/relay*_test.go` | Added focused command tests without real Feishu/Lark network or production daemon use. Coverage raised for `cmd/cc-connect` from 31.1% to 37.6%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p8.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P9 | `done` | `cmd/cc-connect/daemon*_test.go`, `cmd/cc-connect/update*_test.go`, `cmd/cc-connect/feishu*_test.go` | Coverage raised for `cmd/cc-connect` from 37.6% to 45.3%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./cmd/cc-connect`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p9.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |

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
| E-1 Command dispatch | `done` | Added focused tests for builtin slash command dispatch, unknown command fallback, disabled command denial, privileged permission denial, invalid `/lang` args, localized language switching, custom prompt commands, disabled custom commands, exec-command permission checks, and `/commands` add/list/addexec/del branches. |
| E-2 Session orchestration | `done` | Added focused tests for new session start, resume with saved agent ID, invalid session ID clearing, missing agent start failure unlock behavior, and workspace binding routing to workspace-scoped agent/session manager. |
| E-3 Timer/cron execution | `done` | P10 added focused scheduler tests for scheduled target resolution, cron disable/execute paths, timer cancellation, and workspace/work_dir agent resolution. |
| E-4 Card actions | `done` | P15, P17, and P24 covered card action session/workspace binding fallback, card rendering/action branches, and permission response flow. |
| E-5 Outbound and relay | `done` | Added focused tests for outbound queue metadata and queue-full notices, outgoing rate-limit cancellation, proactive thread reply context reconstruction, send failure recovery, relay binding/target validation, relay visibility echo failure recovery, and bridge button/typing/media capability paths. |
| E-6 Workspace flow | `done` | P14 added workspace/project state tests for binding persistence and recovery, corrupt JSON recovery, external binding deletion refresh, and workspace pool behavior. |
| E-7 API and webhook | `done` | P12 added focused API/webhook tests for HTTP method boundaries, malformed payloads, and project/session resolution. |
| E-8 Management | `done` | P12 added focused management tests for static SPA fallback, setup save, add-platform, and settings/project error paths. |

Parallel work packages:

| Work Package | Status | Write Scope | Notes |
| --- | --- | --- | --- |
| P10 | `done` | `core/cron*_test.go`, `core/timer*_test.go`, `core/engine_scheduler*_test.go` | Added focused scheduler tests for scheduled target resolution, mute/silent start notices, workspace/work_dir agent resolution, new side sessions, busy reuse sessions, cron disable/execute paths, timer cancellation, project-missing errors, timer defaults, and mute/store helpers. Core coverage raised to 61.1%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p10.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P11 | `done` | `core/engine*_test.go`, `core/command*_test.go`, `core/session*_test.go` | Added `core/engine_dispatch_session_test.go` with fake agent/platform/session coverage for E-1 command dispatch and E-2 session orchestration. Core coverage raised to 61.4%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p11.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate package rerun and the second full-suite run passed. |
| P12 | `done` | `core/api*_test.go`, `core/webhook*_test.go`, `core/management*_test.go` | Added focused API/webhook/management tests for HTTP method boundaries, malformed payloads, project/session resolution, static SPA fallback, setup save, add-platform, and settings/project error paths. Core coverage raised to 61.9%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p12.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P13 | `done` | `core/relay*_test.go`, `core/bridge*_test.go`, `core/engine_outbound*_test.go` | Added focused outbound/relay/bridge tests for queue metadata and queue-full replies, rate-limit cancellation before send, thread-shaped proactive send reconstruction, send failure recovery without sideText mutation, relay binding/target errors, visibility echo failure isolation, and bridge buttons/typing/media capability support/fallbacks. Core coverage raised to 62.3%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p13.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient P2-package fake CLI timeouts in antigravity/gemini/kimi; those packages passed on immediate package rerun and the second full-suite run passed. |
| P14 | `done` | `core/workspace_binding*_test.go`, `core/projectstate*_test.go`, `core/workspace_state*_test.go` | Added workspace/project state tests for binding persistence and recovery, corrupt JSON recovery, external binding deletion refresh, project state clear/persist behavior, workspace pool normalized lookup, idle reap disablement, active-turn underflow protection, and `All()` snapshot behavior. Core coverage raised to 62.5%; P14 target functions improved notably (`FlexTime.UnmarshalJSON`, `workspaceChannelKeyCandidates`, `workspacePool.Get`, and `workspacePool.All` reached 100%; project state clear/model/load paths reached 90%-100%). Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p14.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate package rerun and the second full-suite run passed. |

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
| F-1 Event parser | `done` | P15 added replayed Feishu SDK event payload tests for message receive with mention, image/file attachments, reaction events, unknown events, malformed JSON, and partial events. |
| F-2 Message dispatch | `done` | P15 and P18 covered message receive/session policy paths plus Feishu helper dispatch branches without real Feishu/Lark network. |
| F-3 Send API | `done` | P16 added local httptest coverage for text/card/image/file send paths, API error and rate-limit handling, token refresh, patch-once, and resource download. |
| F-4 Rich cards | `done` | P17 added focused tests for progress payload normalization, rich-card tool/result rendering, markdown/table helpers, and card rendering branches. |
| F-5 Reply chain | `done` | P18 added Feishu helper branch coverage including reply-chain formatting and withdrawn-message detection. |
| F-6 Image batch | `done` | P18 added focused image-batch tests for replacement flushes, stale timer refs, Stop-time flushing, and canonical dispatch. |
| F-7 Session policy | `done` | P15 added session policy tests for new/reused thread sessions, shared channel reuse, group visibility keys, and card action session/workspace binding fallback. |
| F-8 WebSocket shared | `done` | P18 added shared WebSocket tests for domain isolation, duplicate unregister safety, snapshot independence, and concurrent register/unregister. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P15 | `done` | `platform/feishu/event_parser*_test.go`, `platform/feishu/session_policy*_test.go` | Added replayed Feishu SDK event payload tests for message receive with mention, image/file attachments, reaction events, unknown events, malformed JSON, and partial events; added session policy tests for new/reused thread sessions, shared channel reuse, group visibility keys, and card action session/workspace binding fallback. Coverage raised for `platform/feishu` from 55.0% to 55.2%; `event_parser.go` and `session_policy.go` target functions reached 100% in the package coverprofile. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p15.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate rerun and the second full-suite run passed. |
| P16 | `done` | `platform/feishu/send_api*_test.go`, `platform/feishu/token_retry*_test.go`, `platform/feishu/transient_retry*_test.go` | Added local httptest coverage for text/card/image/file send paths, API error and rate-limit handling, tenant token refresh on upload, patch-once, resource download, and send API helper branches without real Feishu/Lark network or daemon use. Coverage raised for `platform/feishu` from 55.2% to 57.4%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p16.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate rerun and the second full-suite run passed. |
| P17 | `done` | `platform/feishu/rich_card*_test.go`, `platform/feishu/card*_test.go`, `platform/feishu/delete_mode_form*_test.go` | Added focused local tests for progress payload normalization, rich-card tool/result rendering, markdown/table helpers, card send/reply/refresh branches, renderCard/renderCardMap/delete-mode boundaries, and delete mode form value parsing. Coverage raised for `platform/feishu` from 57.4% to 62.2%; P17 target functions improved notably (`ReplyCard`, `SendCard`, `RefreshCard`, `buildProgressCardJSONFromPayload`, `renderProgressEntryElement`, `isTruthyFormValue`, and `buildCardJSONWithStatus` reached 100%). Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p17.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate rerun and the second full-suite run passed. |
| P18 | `done` | `platform/feishu/image_batch*_test.go`, `platform/feishu/ws_shared*_test.go`, `platform/feishu/feishu*_test.go` | Added focused local tests for image-batch replacement flushes, stale timer refs, Stop-time flushing, canonical image batch dispatch, shared WebSocket domain isolation, duplicate unregister safety, snapshot independence, concurrent register/unregister, and Feishu helper branches for bot menu dispatch, reply-chain formatting, withdrawn-message detection, and MIME detection. Coverage raised for `platform/feishu` from 62.2% to 64.9%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./platform/feishu`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p18.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi/traex; those packages passed on immediate rerun and the second full-suite run passed. |

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
| I-1 Config | `done` | Added local config tests for permissive TOML loading, invalid TOML/types, defaults, env placeholders, path expansion, MiniMax secret-redaction on parse errors, display/shell/card helpers, provider CRUD/provider_refs, global settings, web admin defaults, and project setting compatibility. |
| I-2 Daemon | `done` | Added local daemon tests for launchd manager selection/start/stop/uninstall with stubbed `launchctl`, metadata deletion/invalid JSON, and forced log rotation/closed-writer paths without touching production daemon state. |
| I-3 Atomic/filesystem | `done` | P21 added focused tests for atomic write missing dirs, permission failures, rename cleanup, and concurrent complete-payload writes. |
| I-4 Redact/i18n | `done` | P21 added focused tests for env/arg secret redaction, token/app secret/password variants, i18n missing-key and fallback behavior, and auto-language persistence. |
| I-5 Markdown/reference | `done` | Added local tests for Markdown stripping branches, protected bare URLs/web markdown links, code fence preservation, local reference parsing/rendering, invalid references, reference view errors, file range truncation, directory listing boundaries, code fence language detection, and reference display marker/enclosure helpers. |
| I-6 Rate limit | `done` | P21 added focused tests for rate-limit burst, reset, concurrency, and zero/negative config. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P19 | `done` | `config/*_test.go` | Coverage raised for `config` from 66.5% to 81.0%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./config`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./config`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p19.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/gemini` and `agent/kimi`; both packages passed on immediate rerun and the second full-suite run passed. |
| P20 | `done` | `daemon/*_test.go` | Coverage raised for `daemon` from 74.0% to 84.5%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./daemon`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./daemon`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p20.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/antigravity`, `agent/gemini`, and `agent/kimi`; those packages passed on immediate rerun and the second full-suite run passed. |
| P21 | `done` | `core/atomicwrite*_test.go`, `core/redact*_test.go`, `core/i18n*_test.go`, `core/ratelimit*_test.go` | Added focused local tests for atomic write missing dirs, permission failures, rename cleanup, concurrent complete-payload writes, env/arg secret redaction including token/app secret/password variants, i18n missing-key and fallback behavior, auto-language persistence, rate-limit burst/reset/concurrency, and zero/negative config. Core coverage raised to 62.6%. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p21.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/cursor ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run and first low-functions coverprofile run saw transient fake CLI timeouts in antigravity/cursor/gemini/kimi/traex; affected packages passed on immediate rerun and the second full-suite run passed. |
| P22 | `done` | `core/reference_*_test.go`, `core/markdown*_test.go` | Added focused local tests for Markdown/reference escaping, code fences, protected links, reference parsing/rendering, invalid inputs, file/directory view boundaries, and helper style branches. Core coverage raised from 62.6% to 63.1%; target functions improved notably (`parseUserLocalReference`, `renderEnabled`, `containsFolded`, `TransformLocalReferences`, `replaceProtectedLinks`, `applyReferenceEnclosure`, `readFileContext`, and `minInt` reached 100%; `reference_show.go` helpers improved across render/read/code-fence paths). Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./core`, `GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage-p22.out`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in antigravity/gemini/kimi; affected packages passed on immediate rerun and the second full-suite run passed. |

## Module 6: Integration and Regression Layer

Integration tests should lock cross-module contracts. Do not use this layer to inflate line count.

Target directories:

- `tests/integration`
- `tests/release_local`
- `tests/blackbox`
- `tests/e2e`

| Task | Status | Coverage Target |
| --- | --- | --- |
| R-1 Engine + fake platform | `done` | P23 added a release-local engine scenario where a fake platform message reaches a fake agent session and replies without real Feishu/Lark network, production daemon, or production config. |
| R-2 Multi-workspace | `done` | P27 added local integration regression contracts for multi-workspace run_as_user propagation, and P14 covered workspace binding/state isolation. |
| R-3 Turn contract | `done` | Added release-local turn contract scenarios for visible agent event order, tool call/result display, permission response flow, non-terminal result handling, and error termination without real Feishu/Lark network, production daemon, or production config. |
| R-4 Media pipeline | `done` | Added release-local media pipeline scenarios for inbound image/file delivery to fake agent sessions, queued attachment preservation, outbound text/image/file delivery, upload failure after text fallback, attachment-only file reference fallback, disabled attachment send, and ambiguous session rejection without real Feishu/Lark network, production daemon, or production config. |
| R-5 Config scenarios | `done` | Added release-local config scenarios for loaded-config fake agent/platform initialization, provider selection, platform option injection, sender injection, and attachment-send disabling without real Feishu/Lark network, production daemon, or production config. |
| R-6 Regression cases | `done` | Added local integration regression contracts for queued-message reply-context isolation, Feishu relay thread visibility targeting, and multi-workspace run_as_user/run_as_env propagation without real Feishu/Lark network, production daemon, or production config. |

Parallel work packages:

| Work Package | Status | Write Scope |
| --- | --- | --- |
| P23 | `done` | Added release-local engine scenario for a local fake platform Start-handler path: message enters the engine through the fake platform, reaches a fake agent session, and replies back to the fake platform without real Feishu/Lark network, production daemon, or production config. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./tests/release_local/engine_scenarios`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/gemini` and `agent/kimi`; both packages passed on immediate rerun and the second full-suite run passed. |
| P24 | `done` | Added local fake platform/agent turn contract tests for visible event ordering, tool call/result display, permission allow flow while the agent send is blocked, non-terminal `EventResult{Done:false}` continuation, and `EventError` termination ignoring late results. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./tests/release_local/turn_contract`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/antigravity`, `agent/gemini`, `agent/kimi`, and `agent/traex`; affected packages passed on immediate rerun and the second full-suite run passed. |
| P25 | `done` | Added local fake platform/agent media pipeline tests for upload failure after text fallback and attachment-only saved-file reference fallback, complementing existing image/file reference and outbound media scenarios. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./tests/release_local/media_pipeline`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/gemini ./agent/kimi`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/gemini` and `agent/kimi`; both packages passed on immediate rerun and the second full-suite run passed. |
| P26 | `done` | Added local fake platform/agent config scenario tests for config-driven agent options, provider wiring, platform options, engine sender injection, and attachment-send disabling. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test ./tests/release_local/config_scenarios`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi ./agent/traex`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: first full-suite run saw transient fake CLI timeouts in `agent/antigravity`, `agent/gemini`, `agent/kimi`, and `agent/traex`; affected packages passed on immediate rerun and the second full-suite run passed. |
| P27 | `done` | Added `tests/integration/regression_contracts_test.go` with local fake platform/agent regression contracts for historical queued reply context, relay thread visibility, and multi-workspace run_as_user propagation bugs. Validation passed: `GOCACHE=/private/tmp/cc-connect-go-cache go test -tags=integration ./tests/integration/... -run 'TestIntegration_Regression' -count=1`, `GOCACHE=/private/tmp/cc-connect-go-cache go test -tags=integration ./tests/integration/...`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./tests/integration/... ./tests/blackbox/... ./tests/e2e/...`, `GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/antigravity ./agent/gemini ./agent/kimi`, and `GOCACHE=/private/tmp/cc-connect-go-cache go test ./...` on second full-suite run. Note: the untagged requested directory command reports no integration/e2e packages because those tests are build-tagged; first full-suite run saw transient fake CLI timeouts in `agent/antigravity`, `agent/gemini`, and `agent/kimi`, which passed on immediate package rerun and the second full-suite run passed. |

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
GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./...
GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage.out
```

Line-ratio check:

```bash
tools/coverage/local_stats.sh line-ratio
```

Coverage statistics:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh package-coverage ./...
GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh low-functions 50 /tmp/cc-connect-coverage.out
GOCACHE=/private/tmp/cc-connect-go-cache tools/coverage/local_stats.sh all 50
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
