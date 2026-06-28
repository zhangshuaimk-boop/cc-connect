# CC-Connect Development Guide

## Project Scope

CC-Connect connects AI coding agents to Feishu/Lark. The current platform scope is Feishu/Lark only. Do not add, document, test, or preserve built-in support for other chat platforms during this cleanup.

## Architecture

```text
cmd/cc-connect  -> config, core, agent/*, platform/feishu
agent/*         -> core
platform/feishu -> core
core            -> standard library and shared dependencies only
```

`core/` owns interfaces, sessions, i18n, commands, cards, hooks, and orchestration. `platform/feishu/` owns Feishu/Lark API integration and message/card rendering. Agent packages own process lifecycle and event parsing for their agent type.

## Rules

1. Keep built-in chat platform support limited to `platform/feishu`.
2. Keep `ALL_PLATFORMS := feishu` in `Makefile`.
3. Keep `cmd/cc-connect/plugin_platform_feishu.go` as the only built-in platform plugin.
4. Do not reintroduce docs, screenshots, config examples, package keywords, tests, or helper functions for non-Feishu chat platforms.
5. Core should use capability interfaces instead of Feishu-specific type checks unless the behavior is explicitly Feishu-only and belongs in `platform/feishu`.
6. User-facing strings must go through `core/i18n.go`.
7. Tests must pass before committing: `go test ./...`.

## Branch Workflow

1. Keep local `develop` synchronized with `origin/develop` before starting work.
2. Treat `develop` as the long-lived development integration branch.
3. Do not implement new requirements directly on `develop`; create a worktree branch from `develop` for each requirement.
4. After a worktree branch is complete, merge it back into `develop` and push `develop` to origin.
5. Do not implement requirements on `dev` or modify `dev` directly.

## Key Interfaces

- `Platform`: Feishu/Lark adapter boundary used by the engine.
- `Agent`: AI coding agent adapter boundary.
- `AgentSession`: bidirectional running agent session.
- `CardSender`, `RichCardSupporter`, `ButtonSender`: optional message rendering capabilities.
- `DoctorChecker`, `AgentDoctorInfo`: health-check and CLI metadata capabilities.

## Common Commands

```bash
go test ./...
go test ./config ./core ./platform/feishu
go test ./tests/e2e/... -tags=regression
make build
```

## Adding Features

- Add unit tests for new behavior.
- Add a regression test for every bug fix.
- For changes in `core/engine.go`, `core/session.go`, command handlers, cron, or timer behavior, run `go test ./core -run TestCUJ`.
- Update `config.example.toml` and `docs/feishu.md` when configuration or Feishu setup changes.

## Pre-Commit Checklist

1. `go test ./...`
2. `make build`
3. Search docs and package metadata for removed chat platform names.
4. No secrets, tokens, app secrets, or local credentials in source files.
