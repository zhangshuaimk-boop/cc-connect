# TraeX System Prompt Injection Fix Plan

## Problem

`agent.type = "traex"` does not apply `system_prompt`, `append_system_prompt`, or Feishu `FormattingInstructions()` to TraeX sessions.

Observed behavior:

- TraeX loads workspace `AGENTS.md` into model-visible context.
- A unique marker configured through cc-connect project prompt options was not visible in an independent Feishu topic after daemon restart.
- Feishu formatting instructions are expected to affect TraeX agents, but they are not visible to the agent.

## Evidence

TraeX CLI:

- `traex exec --help` does not expose Claude Code style flags such as `--system-prompt` or `--append-system-prompt-file`.
- `traex exec` supports repeated `-c key=value` config overrides.
- TraeX guide documents `instructions` as a config field appended to the model prompt.
- TraeX guide documents `AGENTS.md` as project instructions loaded into model-visible context.

cc-connect code:

- `agent/traex/traex.go` does not store `system_prompt`, `append_system_prompt`, or platform prompt fields.
- `agent/traex/traex.go` `New(opts)` does not read `opts["system_prompt"]` or `opts["append_system_prompt"]`.
- `agent/traex/session.go` `buildExecArgs` does not pass an `instructions` override to TraeX.
- `agent/traex.Agent` does not implement `core.PlatformPromptInjector`, so `core/engine.go` skips Feishu formatting prompt injection for TraeX.
- `agent/codex` already has a fallback prompt preamble for `system_prompt` and `append_system_prompt`.
- `agent/claudecode` already uses native `--system-prompt` and `--append-system-prompt-file`, and implements `SetPlatformPrompt`.

## Root Cause

The TraeX adapter relies on TraeX project instruction loading (`AGENTS.md`) and does not bridge cc-connect project prompt configuration into TraeX runtime configuration.

This makes three prompt channels ineffective for TraeX:

- cc-connect `system_prompt`
- cc-connect `append_system_prompt`
- platform-provided formatting instructions such as Feishu/Lark channel facts

## Goals

- Make `system_prompt` effective for `agent.type = "traex"`.
- Make `append_system_prompt` effective for `agent.type = "traex"`.
- Make Feishu `FormattingInstructions()` effective for `agent.type = "traex"`.
- Preserve existing TraeX `AGENTS.md` behavior.
- Avoid leaking prompt content into logs.
- Keep resume behavior consistent across turns.

## Non-Goals

- Do not change production config semantics.
- Do not change Claude Code, Codex, or other agent adapters.
- Do not require users to copy cc-connect prompts into `AGENTS.md`.
- Do not add new Feishu platform behavior beyond prompt injection.
- Do not depend on real Feishu/Lark network calls in unit tests.

## Proposed Design

### 1. Add prompt fields to `agent/traex.Agent`

Add fields:

```go
systemPrompt string
appendPrompt string
platformPrompt string
```

Read project config in `New(opts)`:

```go
systemPrompt, _ := opts["system_prompt"].(string)
appendPrompt, _ := opts["append_system_prompt"].(string)
```

Trim stored values to avoid blank prompts.

### 2. Implement `core.PlatformPromptInjector`

Add:

```go
func (a *Agent) SetPlatformPrompt(prompt string) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.platformPrompt = strings.TrimSpace(prompt)
}
```

`core/engine.go` already calls this before `StartSession` when the agent implements the interface.

### 3. Build a TraeX instruction string

Create a helper in `agent/traex/session.go` or `agent/traex/traex.go`:

```go
func buildTraexInstructions(systemPrompt, platformPrompt, appendPrompt string) string
```

Suggested structure:

```text
Project system prompt:
<system_prompt>

## Formatting
<platform_prompt>

Additional project instructions:
<append_system_prompt>
```

Rules:

- Omit empty sections.
- Put platform prompt under `## Formatting` to match the Claude Code adapter contract.
- Preserve user-configured `system_prompt` text as a project-level instruction, not as user content.

### 4. Pass instructions through TraeX config override

TraeX supports `-c key=value`. Pass the merged instructions as TOML string:

```text
-c instructions="<escaped merged prompt>"
```

Implementation detail:

- Use TOML quoting via a helper instead of manual escaping.
- Prefer a small local helper if no existing config/TOML utility is appropriate.
- Do not include the prompt in debug logs. Existing `core.RedactArgs(args)` must redact or avoid printing the full `-c instructions=...` value; add redaction if needed.

### 5. Apply instructions on new and resumed turns

Pass the same `instructions` override for both:

- `traex exec ... -`
- `traex exec resume ... <thread_id> ... -`

Reason:

- It is not yet proven that TraeX persists runtime `instructions` across `resume`.
- Re-sending the config override makes behavior deterministic.

### 6. Do not use stdin preamble as first choice

Avoid prepending prompt text to the user message unless TraeX config override proves unusable.

Reason:

- It downgrades system instructions into user content.
- It is less reliable against model instruction hierarchy.
- It differs from TraeX documented `instructions` support.

## Implementation Tasks

| Task | Files | Required Outcome |
| --- | --- | --- |
| T1 | `agent/traex/traex.go` | Store `system_prompt` and `append_system_prompt`; copy them in `StartSession`. |
| T2 | `agent/traex/traex.go` | Implement `SetPlatformPrompt`. |
| T3 | `agent/traex/session.go` | Add merged instruction helper and pass `-c instructions=...` to new and resumed exec args. |
| T4 | `agent/traex/*_test.go` | Add unit tests for config parsing, platform prompt injection, new-session args, and resume args. |
| T5 | `agent/traex/*_test.go` or shared arg redaction tests | Ensure debug arg redaction does not expose instruction body. |
| T6 | `config.example.toml` if needed | Only update if TraeX-specific prompt behavior needs documented config examples. |

## Test Matrix

### Unit Tests

Run:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./agent/traex
GOCACHE=/private/tmp/cc-connect-go-cache go test ./core -run TestCUJ
GOCACHE=/private/tmp/cc-connect-go-cache go test ./platform/feishu ./agent/claudecode ./agent/codex ./agent/traex
```

Required assertions:

- `New(opts)` captures `system_prompt`.
- `New(opts)` captures `append_system_prompt`.
- `SetPlatformPrompt` updates the prompt used by later `StartSession`.
- Fresh TraeX sessions include `-c instructions=...`.
- Resumed TraeX sessions include `-c instructions=...`.
- Merged instructions include `system_prompt`, `## Formatting`, platform prompt, and `append_system_prompt` when all are set.
- Empty prompts do not add `-c instructions=...`.
- Argument redaction hides the instruction content.

### Build

Run:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache make build
```

### Full Regression

Run after package tests pass:

```bash
GOCACHE=/private/tmp/cc-connect-go-cache go test ./...
```

### Real Feishu E2E

Use the independent test instance, not production.

Preconditions:

- Build test binary in the source checkout or worktree.
- Use an isolated test config and test data directory.
- Use test app and test group only.
- Do not restart or mutate production daemon.

E2E steps:

1. Set a unique, never-before-mentioned marker in the test TraeX project `system_prompt`.
2. Start the independent test instance.
3. In a fresh Feishu topic, @ the test TraeX agent and ask whether it can see the marker.
4. The user message must not contain the marker value.
5. Confirm the agent can output the marker exactly.
6. Repeat with a Feishu formatting marker injected from `platform/feishu.FormattingInstructions()` if using an env-gated test build.

Acceptance:

- Marker from `system_prompt` is visible in a fresh TraeX session.
- Marker from Feishu formatting instructions is visible in a fresh TraeX session.
- The same markers remain visible after a resumed turn.
- Normal `AGENTS.md` project instructions still load.
- No prompt body appears in cc-connect logs.

## Risks

| Risk | Mitigation |
| --- | --- |
| TraeX `instructions` config key changes or is not honored by `exec` | Verify with a minimal local `traex exec -c instructions=...` probe before final implementation. |
| Prompt content leaks through debug logs | Extend `core.RedactArgs` or add TraeX-specific redaction tests. |
| `instructions` override replaces user/global instructions instead of appending | Validate with local probe and inspect TraeX docs. If replacement semantics are harmful, use a managed project instruction file fallback. |
| Resume turns lose instructions | Always pass `-c instructions=...` on resume and test resumed turns. |
| Very long prompt exceeds command-line limits | If needed, move to a TraeX profile/config-file approach or managed temporary `AGENTS.override.md` fallback. |

## Fallback Design

If `-c instructions=...` does not work reliably:

1. Write a managed cc-connect instruction file under the project work dir, guarded by a marker.
2. Prefer `AGENTS.override.md` only if it does not overwrite user-owned content.
3. Otherwise append a managed section to `AGENTS.md` through existing memory-file setup flow.
4. Keep cleanup and conflict behavior explicit.

This fallback should be a second phase because TraeX documents `instructions`, and config override is less invasive than modifying project files.
