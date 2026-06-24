# Changelog

## Unreleased

### Changed
- cc-connect is now scoped to Feishu/Lark only.
- Removed non-Feishu platform code paths, docs, examples, metadata, tests, and setup UI references.
- Simplified configuration and onboarding docs around the Feishu/Lark WebSocket flow.

### Validation
- Build and end-to-end validation must use an isolated test instance, not the production daemon.
