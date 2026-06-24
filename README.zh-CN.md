# cc-connect

cc-connect 将本地 AI 编码 Agent 连接到飞书/Lark。用户可以在飞书聊天里操作 Claude Code、Codex、Gemini CLI、Cursor、Kimi、OpenCode、Qoder 等 Agent，同时 Agent 仍运行在用户自己的机器上。

## 功能

- 飞书/Lark 机器人适配器，支持 WebSocket 事件投递。
- 多项目配置：每个项目可绑定独立的工作目录、Agent、Provider 和飞书应用凭证。
- 飞书富卡片、流式预览、文件/图片/音频/视频回传、斜杠命令、会话切换、定时任务、hooks 和管理 API。
- 支持 Provider 预设和按项目配置 Provider。
- 支持 launchd、systemd、Windows task scheduler 的本地 daemon。

## 安装

```bash
npm install -g cc-connect
```

也可以从源码构建：

```bash
git clone https://github.com/chenhg5/cc-connect.git
cd cc-connect
make build
```

## 快速开始

创建或绑定飞书/Lark 机器人：

```bash
cc-connect feishu setup
```

也可以手动编辑配置：

```bash
mkdir -p ~/.cc-connect
cp config.example.toml ~/.cc-connect/config.toml
vim ~/.cc-connect/config.toml
cc-connect -config ~/.cc-connect/config.toml
```

最小项目示例：

```toml
[[projects]]
name = "my-project"

[projects.agent]
type = "codex"

[projects.agent.options]
work_dir = "/path/to/my-project"

[[projects.platforms]]
type = "feishu" # 或 "lark"

[projects.platforms.options]
app_id = "${FEISHU_APP_ID}"
app_secret = "${FEISHU_APP_SECRET}"
allow_from = "*"
```

## 文档

- [飞书接入指南](docs/feishu.md)
- [Management API](docs/management-api.md)
- [中文管理 API](docs/management-api.zh-CN.md)

## 开发

```bash
go test ./...
make build
```

当前唯一内置聊天平台是飞书/Lark。飞书专注重构期间，不应新增其他聊天平台。
