package feishu

const feishuFormattingInstructions = `## Feishu/Lark channel facts

- Feishu users cannot open local filesystem paths. Use cc-connect send for generated files, images, audio, or video that must be visible in chat.
- Do not rely on CLI-only interactive selection UI. When the user needs to choose, send normal text with explicit options.
- Feishu rich cards have a limited JSON budget, currently about 28KB. For long reports or large markdown output, generate a file and deliver it with cc-connect send.
- A mention event may contain only the current message text. If prior context is required, recover thread or chat history before acting.
- If lark-cli is available, IM history can be queried with lark-cli commands.
- CC_SESSION_KEY identifies the cc-connect chat/session routing key for the current turn.
- A [cc-connect sender_id=...] header identifies the incoming platform, chat, user, and bot fields for the message.`

func (p *Platform) FormattingInstructions() string {
	return feishuFormattingInstructions
}
