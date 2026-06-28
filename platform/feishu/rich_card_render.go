package feishu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

// buildCardJSON builds a Feishu interactive card JSON string with a markdown element.
// Uses schema 2.0 which supports code blocks, tables, and inline formatting.
// Card font is inherently smaller than Post/Text — this is a Feishu platform limitation.
func buildCardJSON(content string) string {
	content = sanitizeCardMarkdownForCard(content)
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

// buildCardJSONWithStatusFooter builds an interactive card with a body
// markdown element followed by a small/dim status-footer markdown element
// (Lark `text_size: "notation"`). Empty footer falls through to buildCardJSON.
func buildCardJSONWithStatusFooter(content, footer string) string {
	if strings.TrimSpace(footer) == "" {
		return buildCardJSON(content)
	}
	segments := sanitizeCardMarkdownSegmentsForCard([]string{content, footer})
	content = segments[0]
	footer = segments[1]
	elements := []map[string]any{
		{
			"tag":     "markdown",
			"content": content,
		},
		{
			"tag": "hr",
		},
		{
			"tag":       "markdown",
			"content":   footer,
			"text_size": "notation",
		},
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func isZhLikeProgressLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return strings.HasPrefix(l, "zh")
}

func progressAgentLabel(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "Agent"
	}
	return agent
}

func progressStateMeta(state core.ProgressCardState, lang string, agent string) (title string, template string, footer string) {
	zh := isZhLikeProgressLang(lang)
	switch state {
	case core.ProgressCardStateCompleted:
		if zh {
			return fmt.Sprintf("%s · 已完成", agent), "green", "本过程卡片已停止更新，完整答复见下一条消息。"
		}
		return fmt.Sprintf("%s · Completed", agent), "green", "This progress card is no longer updating. Full response is in the next message."
	case core.ProgressCardStateFailed:
		if zh {
			return fmt.Sprintf("%s · 失败", agent), "red", "本过程卡片已停止更新（失败），完整错误说明见下一条消息。"
		}
		return fmt.Sprintf("%s · Failed", agent), "red", "This progress card has stopped (failed). See the next message for details."
	default:
		if zh {
			return fmt.Sprintf("%s · 进行中", agent), "blue", ""
		}
		return fmt.Sprintf("%s · Running", agent), "blue", ""
	}
}

func progressKindLabel(kind core.ProgressCardEntryKind, lang string) string {
	zh := isZhLikeProgressLang(lang)
	switch kind {
	case core.ProgressEntryThinking:
		if zh {
			return "思考"
		}
		return "Thinking"
	case core.ProgressEntryToolUse:
		if zh {
			return "工具调用"
		}
		return "Tool"
	case core.ProgressEntryToolResult:
		if zh {
			return "工具结果"
		}
		return "Result"
	case core.ProgressEntryError:
		if zh {
			return "错误"
		}
		return "Error"
	default:
		if zh {
			return "更新"
		}
		return "Update"
	}
}

func normalizeProgressItems(payload *core.ProgressCardPayload) []core.ProgressCardEntry {
	if payload == nil {
		return nil
	}
	if len(payload.Items) > 0 {
		return payload.Items
	}
	out := make([]core.ProgressCardEntry, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		kind := core.ProgressEntryInfo
		switch {
		case strings.HasPrefix(entry, "💭"):
			kind = core.ProgressEntryThinking
		case strings.HasPrefix(entry, "🔧"), strings.Contains(entry, "**Tool #"):
			kind = core.ProgressEntryToolUse
		case strings.HasPrefix(entry, "🧾"):
			kind = core.ProgressEntryToolResult
		case strings.HasPrefix(entry, "❌"):
			kind = core.ProgressEntryError
		}
		out = append(out, core.ProgressCardEntry{Kind: kind, Text: entry})
	}
	return out
}

func inlineCodeText(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
}

func isBashToolName(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash", "shell", "run_shell_command":
		return true
	default:
		return false
	}
}

func isTodoWriteToolName(toolName string) bool {
	return strings.EqualFold(strings.TrimSpace(toolName), "todowrite")
}

// todoItem represents a single todo item from TodoWrite tool input.
type todoItem struct {
	ActiveForm string `json:"activeForm"`
	Content    string `json:"content"`
	Status     string `json:"status"`
}

// todoWriteInput represents the TodoWrite tool input structure.
type todoWriteInput struct {
	Todos []todoItem `json:"todos"`
}

// formatTodoWriteInput formats TodoWrite JSON input into a readable markdown list.
// Returns empty string if parsing fails or input is invalid.
func formatTodoWriteInput(text string, lang string) string {
	var input todoWriteInput
	if err := json.Unmarshal([]byte(text), &input); err != nil {
		return "" // Fall back to default formatting
	}
	if len(input.Todos) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, todo := range input.Todos {
		var icon string
		switch strings.ToLower(strings.TrimSpace(todo.Status)) {
		case "completed":
			icon = "✅"
		case "in_progress":
			icon = "🔄"
		case "pending":
			icon = "⏳"
		default:
			icon = "•"
		}

		content := strings.TrimSpace(todo.Content)
		if content == "" {
			continue
		}

		// Escape markdown special characters
		content = strings.ReplaceAll(content, "`", "'")

		sb.WriteString(icon)
		sb.WriteString(" ")
		sb.WriteString(content)

		activeForm := strings.TrimSpace(todo.ActiveForm)
		if activeForm != "" && activeForm != content {
			sb.WriteString(" _(")
			sb.WriteString(strings.ReplaceAll(activeForm, "`", "'"))
			sb.WriteString(")_")
		}
		sb.WriteString("\n")
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

func formatProgressToolInput(toolName, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Special handling for TodoWrite tool - format JSON as readable list
	if isTodoWriteToolName(toolName) {
		if formatted := formatTodoWriteInput(text, ""); formatted != "" {
			return formatted
		}
		// JSON parsing failed or empty todos - show raw input as text block
		return fmt.Sprintf("```text\n%s\n```", text)
	}

	text = preprocessFeishuMarkdown(sanitizeMarkdownURLs(text))
	if strings.Contains(text, "```") {
		return text
	}
	if isBashToolName(toolName) {
		return fmt.Sprintf("```bash\n%s\n```", text)
	}
	if strings.Contains(text, "\n") || len(text) > 180 {
		return fmt.Sprintf("```text\n%s\n```", text)
	}
	return fmt.Sprintf("`%s`", inlineCodeText(text))
}

func formatProgressToolResult(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = preprocessFeishuMarkdown(sanitizeMarkdownURLs(text))
	if strings.Contains(text, "```") {
		return text
	}
	if strings.Contains(text, "\n") || len(text) > 220 {
		return fmt.Sprintf("```\n%s\n```", text)
	}
	return text
}

func progressNoOutputText(lang string) string {
	if isZhLikeProgressLang(lang) {
		return "无输出"
	}
	return "No output"
}

func progressResultDot(item core.ProgressCardEntry) string {
	if item.Success != nil {
		if *item.Success {
			return "🟢"
		}
		return "🔴"
	}
	if item.ExitCode != nil {
		if *item.ExitCode == 0 {
			return "🟢"
		}
		return "🔴"
	}
	if strings.EqualFold(strings.TrimSpace(item.Status), "completed") || strings.EqualFold(strings.TrimSpace(item.Status), "success") || strings.EqualFold(strings.TrimSpace(item.Status), "succeeded") || strings.EqualFold(strings.TrimSpace(item.Status), "ok") {
		return "🟢"
	}
	if strings.EqualFold(strings.TrimSpace(item.Status), "failed") || strings.EqualFold(strings.TrimSpace(item.Status), "error") {
		return "🔴"
	}
	return "⚪"
}

func progressToolElement(iconToken string, content string) map[string]any {
	elem := map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "lark_md",
			"content": content,
		},
	}
	if iconToken != "" {
		elem["icon"] = map[string]any{"tag": "standard_icon", "token": iconToken}
	}
	return elem
}

func renderProgressEntryElement(item core.ProgressCardEntry, lang string) map[string]any {
	text := strings.TrimSpace(item.Text)
	if text == "" {
		text = " "
	}
	switch item.Kind {
	case core.ProgressEntryThinking:
		return map[string]any{
			"tag":  "div",
			"icon": map[string]any{"tag": "standard_icon", "token": reasoningToolIcon},
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    "💭 " + inlineCodeText(text),
				"text_size":  "notation",
				"text_color": "grey",
			},
		}
	case core.ProgressEntryToolUse:
		toolName := strings.TrimSpace(item.Tool)
		if toolName == "" {
			toolName = "Tool"
		}
		display := buildToolDisplay(toolName, text)
		content := fmt.Sprintf("<text_tag color='blue'>%s</text_tag> **%s**", progressKindLabel(item.Kind, lang), inlineCodeText(display.Title))
		if body := formatProgressToolInput(toolName, display.Detail); body != "" {
			content += "\n" + body
		}
		return progressToolElement(display.IconToken, content)
	case core.ProgressEntryToolResult:
		toolName := strings.TrimSpace(item.Tool)
		display := buildToolDisplay(toolName, "")
		content := fmt.Sprintf("<text_tag color='turquoise'>%s</text_tag> **%s**", progressKindLabel(item.Kind, lang), inlineCodeText(display.Title))
		dot := progressResultDot(item)
		meta := dot
		if item.ExitCode != nil {
			meta += fmt.Sprintf(" exit code: `%d`", *item.ExitCode)
		}
		content += "\n" + meta
		if body := formatProgressToolResult(sanitizeToolDetail(toolSanitizerGeneric, item.Text)); body != "" {
			content += "\n" + body
		} else {
			content += "\n_" + progressNoOutputText(lang) + "_"
		}
		return progressToolElement(display.IconToken, content)
	case core.ProgressEntryError:
		content := fmt.Sprintf("<text_tag color='red'>%s</text_tag>\n%s", progressKindLabel(item.Kind, lang), sanitizeCardMarkdownForCard(text))
		return map[string]any{
			"tag":     "markdown",
			"content": content,
		}
	default:
		return map[string]any{
			"tag":     "markdown",
			"content": sanitizeCardMarkdownForCard(text),
		}
	}
}

func splitProgressItemsByLane(items []core.ProgressCardEntry) (reasoning []core.ProgressCardEntry, tools []core.ProgressCardEntry, others []core.ProgressCardEntry) {
	for _, item := range items {
		switch item.Kind {
		case core.ProgressEntryThinking:
			reasoning = append(reasoning, item)
		case core.ProgressEntryToolUse, core.ProgressEntryToolResult:
			tools = append(tools, item)
		default:
			others = append(others, item)
		}
	}
	return reasoning, tools, others
}

func progressPanelTitle(label string, count int, lang string) string {
	if isZhLikeProgressLang(lang) {
		switch label {
		case "Reasoning":
			label = "思考"
		case "Tools":
			label = "工具"
		case "Updates":
			label = "更新"
		}
	}
	if count > 0 {
		return fmt.Sprintf("%s (%d)", label, count)
	}
	return label
}

func buildProgressPanel(title string, expanded bool, elements []map[string]any) map[string]any {
	return map[string]any{
		"tag":              "collapsible_panel",
		"expanded":         expanded,
		"background_color": "grey",
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": title},
		},
		"border":           map[string]any{"color": "grey"},
		"vertical_spacing": "8px",
		"padding":          "4px 8px",
		"elements":         elements,
	}
}

func buildProgressPanelElements(items []core.ProgressCardEntry, lang string) []map[string]any {
	elements := make([]map[string]any, 0, len(items))
	for _, item := range items {
		elements = append(elements, renderProgressEntryElement(item, lang))
	}
	return elements
}

func appendProgressGroupedElements(elements []map[string]any, items []core.ProgressCardEntry, lang string, running bool) []map[string]any {
	reasoning, tools, others := splitProgressItemsByLane(items)
	if len(reasoning) > 0 {
		elements = append(elements, buildProgressPanel(
			progressPanelTitle("Reasoning", len(reasoning), lang),
			running,
			buildProgressPanelElements(reasoning, lang),
		))
	}
	if len(tools) > 0 {
		elements = append(elements, buildProgressPanel(
			progressPanelTitle("Tools", len(tools), lang),
			running,
			buildProgressPanelElements(tools, lang),
		))
	}
	if len(others) > 0 {
		elements = append(elements, buildProgressPanel(
			progressPanelTitle("Updates", len(others), lang),
			running,
			buildProgressPanelElements(others, lang),
		))
	}
	return elements
}

func buildProgressCardJSONFromPayload(payload *core.ProgressCardPayload) string {
	items := normalizeProgressItems(payload)
	if len(items) == 0 {
		return buildCardJSON(" ")
	}

	agent := progressAgentLabel(payload.Agent)
	title, template, footer := progressStateMeta(payload.State, payload.Lang, agent)
	running := payload.State == core.ProgressCardStateRunning

	elements := make([]map[string]any, 0, len(items)+3)
	if payload.Truncated {
		truncatedText := "Showing latest updates only."
		if isZhLikeProgressLang(payload.Lang) {
			truncatedText = "仅显示最近更新。"
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    truncatedText,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
		elements = append(elements, map[string]any{"tag": "hr"})
	}

	elements = appendProgressGroupedElements(elements, items, payload.Lang, running)
	if footer != "" {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":        "plain_text",
				"content":    footer,
				"text_size":  "notation",
				"text_color": "grey",
			},
		})
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": template,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func buildPreviewCardJSON(content string) string {
	if payload, ok := core.ParseProgressCardPayload(content); ok {
		return buildProgressCardJSONFromPayload(payload)
	}
	return buildCardJSON(sanitizeMarkdownURLs(content))
}

var markdownTablePattern = regexp.MustCompile(`(?m)^\|.+\|\s*\n\|[\s:|-]+\|\s*\n(?:\|.+\|\s*\n?)+`)

type markdownTextMatch struct {
	start int
	end   int
	raw   string
}

type markdownLine struct {
	text       string
	start, end int
}

var feishuCardImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

func markdownLinesWithOffsets(text string) []markdownLine {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	lines := make([]markdownLine, 0, len(parts))
	offset := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		next := offset + len(part)
		lines = append(lines, markdownLine{text: part, start: offset, end: next})
		offset = next
	}
	return lines
}

func isMarkdownTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= 2 && strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

func isMarkdownTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !isMarkdownTableRow(trimmed) {
		return false
	}
	hasDash := false
	for _, r := range trimmed {
		switch r {
		case '|', '-', ':', ' ':
			if r == '-' {
				hasDash = true
			}
		default:
			return false
		}
	}
	return hasDash
}

func findMarkdownTablesOutsideCodeBlocks(text string) []markdownTextMatch {
	lines := markdownLinesWithOffsets(text)
	var matches []markdownTextMatch
	inCodeBlock := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i].text)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock || i+1 >= len(lines) || !isMarkdownTableRow(lines[i].text) || !isMarkdownTableSeparator(lines[i+1].text) {
			continue
		}
		start := lines[i].start
		end := lines[i+1].end
		j := i + 2
		for j < len(lines) && isMarkdownTableRow(lines[j].text) {
			end = lines[j].end
			j++
		}
		matches = append(matches, markdownTextMatch{
			start: start,
			end:   end,
			raw:   strings.TrimSpace(text[start:end]),
		})
		i = j - 1
	}
	return matches
}

func wrapTablesBeyondLimit(text string, matches []markdownTextMatch, keepCount int) string {
	if len(matches) <= keepCount {
		return text
	}
	if keepCount < 0 {
		keepCount = 0
	}
	result := text
	for i := len(matches) - 1; i >= keepCount; i-- {
		match := matches[i]
		replacement := "```\n" + match.raw + "\n```"
		result = result[:match.start] + replacement + result[match.end:]
	}
	return result
}

func sanitizeCardMarkdownTables(text string, remainingBudget int) (string, int) {
	matches := findMarkdownTablesOutsideCodeBlocks(text)
	if len(matches) <= remainingBudget {
		return text, remainingBudget - len(matches)
	}
	return wrapTablesBeyondLimit(text, matches, remainingBudget), 0
}

func stripInvalidFeishuCardImages(text string) string {
	if !strings.Contains(text, "![") {
		return text
	}
	return feishuCardImagePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := feishuCardImagePattern.FindStringSubmatch(match)
		if len(parts) == 3 && strings.HasPrefix(parts[2], "img_") {
			return match
		}
		return ""
	})
}

func protectFencedCodeBlocks(text string) (string, []string) {
	var blocks []string
	var b strings.Builder
	for i := 0; i < len(text); {
		start := strings.Index(text[i:], "```")
		if start < 0 {
			b.WriteString(text[i:])
			break
		}
		start += i
		end := strings.Index(text[start+3:], "```")
		if end < 0 {
			b.WriteString(text[i:])
			break
		}
		end += start + 6
		b.WriteString(text[i:start])
		placeholder := fmt.Sprintf("\x00CC_FEISHU_CODE_BLOCK_%d\x00", len(blocks))
		blocks = append(blocks, text[start:end])
		b.WriteString(placeholder)
		i = end
	}
	return b.String(), blocks
}

func restoreFencedCodeBlocks(text string, blocks []string) string {
	for i, block := range blocks {
		placeholder := fmt.Sprintf("\x00CC_FEISHU_CODE_BLOCK_%d\x00", i)
		text = strings.ReplaceAll(text, placeholder, block)
	}
	return text
}

func optimizeFeishuCardMarkdown(text string) string {
	protected, blocks := protectFencedCodeBlocks(text)
	if regexp.MustCompile(`(?m)^#{1,3} `).MatchString(protected) {
		protected = regexp.MustCompile(`(?m)^#{2,6} (.+)$`).ReplaceAllString(protected, "##### $1")
		protected = regexp.MustCompile(`(?m)^# (.+)$`).ReplaceAllString(protected, "#### $1")
	}
	protected = regexp.MustCompile(`\n{3,}`).ReplaceAllString(protected, "\n\n")
	return restoreFencedCodeBlocks(protected, blocks)
}

func sanitizeCardMarkdownSegmentsForCard(texts []string) []string {
	out := make([]string, len(texts))
	remainingBudget := feishuCardTableLimit
	for i, text := range texts {
		prepared := sanitizeMarkdownURLs(preprocessFeishuMarkdown(text))
		prepared = stripInvalidFeishuCardImages(prepared)
		prepared = optimizeFeishuCardMarkdown(prepared)
		prepared, remainingBudget = sanitizeCardMarkdownTables(prepared, remainingBudget)
		out[i] = prepared
	}
	return out
}

func sanitizeCardMarkdownForCard(text string) string {
	return sanitizeCardMarkdownSegmentsForCard([]string{text})[0]
}

func richStepDisplayName(step core.ToolStep) string {
	if step.Kind == core.ToolStepKindThinking {
		return "Thinking"
	}
	return buildToolDisplay(step.Name, step.Summary).Title
}

func richStepBody(step core.ToolStep) string {
	name := richStepDisplayName(step)
	summary := buildToolDisplay(step.Name, step.Summary).Detail
	if summary == "" {
		summary = name
	}
	if step.Kind == core.ToolStepKindThinking {
		return summary
	}

	lines := []string{summary}
	var statusParts []string
	status := strings.TrimSpace(step.Status)
	if status != "" {
		statusParts = append(statusParts, "status: "+status)
	} else if step.Success != nil {
		if *step.Success {
			statusParts = append(statusParts, "status: ok")
		} else {
			statusParts = append(statusParts, "status: failed")
		}
	}
	if step.ExitCode != nil {
		statusParts = append(statusParts, fmt.Sprintf("exit: %d", *step.ExitCode))
	}
	if len(statusParts) > 0 {
		lines = append(lines, strings.Join(statusParts, " | "))
	}
	if result := strings.TrimSpace(step.Result); result != "" {
		lines = append(lines, result)
	}
	return strings.Join(lines, "\n")
}

// isCardJSON returns true if content looks like a complete Feishu card JSON
// (has "schema" and "body"). Used to avoid double-wrapping rich card output.
func isCardJSON(content string) bool {
	if len(content) < 10 || content[0] != '{' {
		return false
	}
	return strings.Contains(content, `"schema"`) && strings.Contains(content, `"body"`)
}

// buildCardJSONWithStatus builds a Feishu card JSON with a colored header
// reflecting the given status. Used as a fallback when rich-card assembly fails.
func buildCardJSONWithStatus(content string, status core.CardStatus) string {
	content = sanitizeCardMarkdownForCard(content)
	template := "grey"
	switch status {
	case core.CardStatusWorking, core.CardStatusThinking:
		template = "blue"
	case core.CardStatusDone:
		template = "green"
	case core.CardStatusError:
		template = "red"
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"width_mode": "default", // schema 2.0 field; was wide_screen_mode (schema 1.0)
		},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": ""},
		},
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}
	b, _ := json.Marshal(card)
	return string(b)
}

func splitRichStepsByLane(steps []core.ToolStep) (reasoning []core.ToolStep, tools []core.ToolStep) {
	for _, step := range steps {
		if step.Kind == core.ToolStepKindThinking {
			reasoning = append(reasoning, step)
			continue
		}
		tools = append(tools, step)
	}
	return reasoning, tools
}

func richLaneTitle(label string, count int) string {
	if count > 0 {
		return fmt.Sprintf("%s (%d)", label, count)
	}
	return label
}

func richStepRowContent(step core.ToolStep) string {
	body := richStepBody(step)
	if step.Kind == core.ToolStepKindThinking {
		return body
	}
	name := richStepDisplayName(step)
	if body == name || strings.HasPrefix(body, name+"\n") {
		return body
	}
	return name + "\n" + body
}

func richStepElement(step core.ToolStep) map[string]any {
	text := map[string]any{
		"tag":       "plain_text",
		"content":   richStepRowContent(step),
		"text_size": "notation",
	}
	elem := map[string]any{
		"tag":  "div",
		"text": text,
	}
	if step.Kind == core.ToolStepKindThinking {
		text["text_color"] = "grey"
		elem["icon"] = map[string]any{"tag": "standard_icon", "token": reasoningToolIcon}
		return elem
	}
	elem["icon"] = map[string]any{"tag": "standard_icon", "token": buildToolDisplay(step.Name, step.Summary).IconToken}
	return elem
}

func richPlaceholderElement(text string) map[string]any {
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":        "plain_text",
			"content":    text,
			"text_size":  "notation",
			"text_color": "grey",
		},
	}
}

func richPanelElements(steps []core.ToolStep, emptyText string) []map[string]any {
	if len(steps) == 0 {
		return []map[string]any{richPlaceholderElement(emptyText)}
	}
	const maxPanelSteps = 10
	visible := steps
	hidden := 0
	if len(steps) > maxPanelSteps {
		hidden = len(steps) - maxPanelSteps
		visible = steps[hidden:]
	}
	elements := make([]map[string]any, 0, len(visible)+1)
	if hidden > 0 {
		elements = append(elements, richPlaceholderElement(fmt.Sprintf("... %d earlier steps hidden", hidden)))
	}
	for _, step := range visible {
		elements = append(elements, richStepElement(step))
	}
	return elements
}

func buildRichPanel(title string, expanded bool, elements []map[string]any) map[string]any {
	return map[string]any{
		"tag":              "collapsible_panel",
		"expanded":         expanded,
		"background_color": "grey",
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": title},
		},
		"border":           map[string]any{"color": "grey"},
		"vertical_spacing": "8px",
		"padding":          "4px 8px",
		"elements":         elements,
	}
}

const maxRichCardJSONBytes = 28000

// buildRichCard renders a Card 2.0 "single-card" turn with collapsible
// reasoning/tool panels, streaming markdown body, status-colored header, and a
// pre-composed multi-line statusFooter (engine-owned, includes elapsed).
func buildRichCard(status core.CardStatus, _ string, steps []core.ToolStep, markdown string, streaming bool, statusFooter string) string {
	b, err := buildRichCardJSONBytes(status, steps, markdown, streaming, statusFooter)
	if err != nil {
		slog.Debug("feishu: build rich card marshal failed, fallback to basic card", "error", err)
		return buildCardJSONWithStatus(markdown, status)
	}
	if len(b) <= maxRichCardJSONBytes {
		return string(b)
	}

	// Keep Card 2.0 visible when long tool/reasoning history would exceed the
	// Feishu payload limit. Dropping to a body-only fallback is unsafe because
	// Codex intermediate messages often live only in panels, leaving markdown
	// empty and producing a blank white card on update.
	for _, limit := range []struct {
		perLane int
		textLen int
	}{
		{perLane: 10, textLen: 180},
		{perLane: 6, textLen: 120},
		{perLane: 3, textLen: 80},
	} {
		compactSteps := compactRichStepsForCardSize(steps, limit.perLane, limit.textLen)
		compact, err := buildRichCardJSONBytes(status, compactSteps, markdown, streaming, statusFooter)
		if err == nil && len(compact) <= maxRichCardJSONBytes {
			slog.Debug("feishu: rich card exceeded size limit, compacted panels",
				"original_size", len(b),
				"compacted_size", len(compact),
				"steps", len(steps),
				"compacted_steps", len(compactSteps),
			)
			return string(compact)
		}
	}

	fallbackMarkdown := markdown
	if strings.TrimSpace(fallbackMarkdown) == "" {
		fallbackMarkdown = compactRichFallbackMarkdown(steps)
	}
	slog.Debug("feishu: rich card exceeds size limit, fallback to compact markdown card", "size", len(b))
	return buildCardJSONWithStatus(fallbackMarkdown, status)
}

func buildRichCardJSONBytes(status core.CardStatus, steps []core.ToolStep, markdown string, streaming bool, statusFooter string) ([]byte, error) {
	reasoningSteps, toolSteps := splitRichStepsByLane(steps)
	panelMaps := make([]map[string]any, 0, 2)
	if len(reasoningSteps) > 0 {
		panelMaps = append(panelMaps, buildRichPanel(
			richLaneTitle("Reasoning", len(reasoningSteps)),
			streaming,
			richPanelElements(reasoningSteps, "Thinking..."),
		))
	}
	if len(toolSteps) > 0 {
		panelMaps = append(panelMaps, buildRichPanel(
			richLaneTitle("Tools", len(toolSteps)),
			streaming,
			richPanelElements(toolSteps, "No tool steps"),
		))
	}
	if len(panelMaps) == 0 && streaming {
		panelMaps = append(panelMaps, buildRichPanel("Reasoning", true, richPanelElements(nil, "Thinking...")))
	}

	markdownMap := map[string]any{
		"tag":        "markdown",
		"element_id": richCardMainTextElementID, // required for cardkit-v1 streaming text update
		"content":    sanitizeCardMarkdownForCard(markdown),
	}

	// Footer: engine pre-composes a multi-line statusFooter (lines separated by \n).
	// Each line renders as its own dim "notation"-sized markdown block so they
	// visually sit below the body without being mistaken for content. Skip
	// rendering when statusFooter is empty (footer disabled / nothing to show).
	var footerElements []map[string]any
	if statusFooter != "" {
		for _, line := range strings.Split(statusFooter, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			footerElements = append(footerElements, map[string]any{
				"tag":       "markdown",
				"content":   sanitizeCardMarkdownForCard(line),
				"text_size": "notation",
			})
		}
	}

	var elements []map[string]any
	if len(panelMaps) > 0 {
		elements = append(elements, panelMaps...)
		elements = append(elements, markdownMap)
	} else {
		elements = append(elements, markdownMap)
	}
	if len(footerElements) > 0 {
		// Insert a horizontal separator between body and footer so the boundary is clear.
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, footerElements...)
	}

	// Header template color follows status.
	headerTemplate := "blue"
	headerTitle := pickThinkingVerb()
	switch status {
	case core.CardStatusDone:
		headerTemplate = "green"
		headerTitle = "Done"
	case core.CardStatusError:
		headerTemplate = "red"
		headerTitle = "Error"
	case core.CardStatusThinking, core.CardStatusWorking:
		headerTemplate = "blue"
		headerTitle = pickThinkingVerb()
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode":             streaming,
			"update_multi":               true,
			"enable_forward_interaction": true,
		},
		"header": map[string]any{
			"template": headerTemplate,
			"title":    map[string]any{"tag": "plain_text", "content": headerTitle},
		},
		"body": map[string]any{"elements": elements},
	}

	return json.Marshal(card)
}

func compactRichStepsForCardSize(steps []core.ToolStep, perLaneLimit, textLimit int) []core.ToolStep {
	if len(steps) == 0 || perLaneLimit <= 0 {
		return nil
	}
	kept := make([]core.ToolStep, 0, min(len(steps), perLaneLimit*2))
	reasoning := 0
	tools := 0
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.Kind == core.ToolStepKindThinking {
			if reasoning >= perLaneLimit {
				continue
			}
			reasoning++
		} else {
			if tools >= perLaneLimit {
				continue
			}
			tools++
		}
		kept = append(kept, compactRichStepText(step, textLimit))
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

func compactRichStepText(step core.ToolStep, textLimit int) core.ToolStep {
	step.Summary = compactRichText(step.Summary, textLimit)
	step.Result = compactRichText(step.Result, textLimit)
	return step
}

func compactRichText(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= maxRunes {
		return string(rs)
	}
	return string(rs[:maxRunes]) + "..."
}

func compactRichFallbackMarkdown(steps []core.ToolStep) string {
	compactSteps := compactRichStepsForCardSize(steps, 3, 120)
	if len(compactSteps) == 0 {
		return ""
	}
	lines := []string{"Card content is large; showing recent activity:"}
	for _, step := range compactSteps {
		line := strings.TrimSpace(richStepRowContent(step))
		if line == "" {
			continue
		}
		line = strings.ReplaceAll(line, "\n", " - ")
		lines = append(lines, "- "+line)
	}
	return strings.Join(lines, "\n")
}

func splitMarkdownByTables(md string, maxTables int) []string {
	if maxTables <= 0 {
		return []string{md}
	}
	matches := markdownTablePattern.FindAllStringIndex(md, -1)
	if len(matches) <= maxTables {
		return []string{md}
	}
	parts := make([]string, 0, len(matches)-maxTables+1)
	firstEnd := len(md)
	if len(matches) > maxTables {
		firstEnd = matches[maxTables][0]
	}
	first := strings.TrimSpace(md[:firstEnd])
	if first != "" {
		parts = append(parts, first)
	}
	for _, match := range matches[maxTables:] {
		block := strings.TrimSpace(md[match[0]:match[1]])
		if block != "" {
			parts = append(parts, block)
		}
	}
	return parts
}
