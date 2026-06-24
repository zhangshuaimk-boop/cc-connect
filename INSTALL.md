# Install cc-connect

cc-connect bridges local AI coding agents to Feishu/Lark.

## Install

```bash
npm install -g cc-connect
```

Or build from source:

```bash
go install github.com/chenhg5/cc-connect/cmd/cc-connect@latest
```

## Configure Feishu/Lark

Create a Feishu/Lark app, enable bot messaging, and use WebSocket event delivery.

Minimal project configuration:

```toml
[[projects]]
name = "my-project"

[projects.agent]
type = "claudecode"
work_dir = "/path/to/workspace"

[[projects.platforms]]
type = "feishu"
app_id = "cli_a..."
app_secret = "..."
```

Start cc-connect with the config file:

```bash
cc-connect --config ./config.toml
```

See [docs/feishu.md](docs/feishu.md) for the Feishu/Lark setup guide.
