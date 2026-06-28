# cc-connect

cc-connect connects local AI coding agents to Feishu/Lark. It lets users operate agents such as Claude Code, Codex, Gemini CLI, Cursor, Kimi, OpenCode, Qoder, and others from Feishu chats while the agents keep running on the user's machine.

## Features

- Feishu/Lark bot adapter with WebSocket event delivery.
- Multi-project configuration: each project can bind its own work directory, agent, providers, and Feishu app credentials.
- Rich Feishu cards, streaming previews, file/image/audio/video send-back, slash commands, session switching, cron jobs, hooks, and management APIs.
- Provider presets and per-project provider configuration for supported agents.
- Local daemon support for launchd, systemd, and Windows task scheduler.

## Install

```bash
npm install -g cc-connect
```

Or build from source:

```bash
git clone https://github.com/chenhg5/cc-connect.git
cd cc-connect
make build
```

## Quick Start

Create or bind a Feishu/Lark bot:

```bash
cc-connect feishu setup
```

Or edit `config.toml` manually:

```bash
mkdir -p ~/.cc-connect
cp config.example.toml ~/.cc-connect/config.toml
vim ~/.cc-connect/config.toml
cc-connect -config ~/.cc-connect/config.toml
```

Minimal project example:

```toml
[[projects]]
name = "my-project"

[projects.agent]
type = "codex"

[projects.agent.options]
work_dir = "/path/to/my-project"

[[projects.platforms]]
type = "feishu" # or "lark"

[projects.platforms.options]
app_id = "${FEISHU_APP_ID}"
app_secret = "${FEISHU_APP_SECRET}"
allow_from = "*"
```

## Documentation

- [Feishu setup guide](docs/feishu.md)
- [Management API](docs/management-api.md)
- [中文管理 API](docs/management-api.zh-CN.md)

## Development

```bash
go test ./...
make build
```

The only built-in chat platform is Feishu/Lark. New platform work should not be added during the Feishu-focused cleanup.
