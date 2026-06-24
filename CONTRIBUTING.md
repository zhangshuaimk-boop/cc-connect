# Contributing to cc-connect

cc-connect currently focuses on Feishu/Lark as its only built-in chat platform.

## Issues

Please include:

- `cc-connect --version`
- OS and installation method
- Agent type
- Feishu/Lark app type and event delivery mode
- Reproduction steps
- Expected and actual behavior
- Logs with secrets redacted

## Pull Requests

- Follow [AGENTS.md](AGENTS.md).
- Run `go test ./...` before submitting.
- Run `make build` for changes that affect packaging, plugins, web assets, or command wiring.
- Update `docs/feishu.md`, `config.example.toml`, and README files when Feishu/Lark setup or configuration changes.
- Do not add non-Feishu chat platform code, tests, docs, screenshots, package keywords, or config examples.

## Release Notes

Call out behavior changes and migration steps explicitly. Feishu/Lark compatibility and existing config stability are the primary compatibility surface.
