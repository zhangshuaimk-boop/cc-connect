package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func decodeCardJSON(t *testing.T, cardJSON string) map[string]any {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal([]byte(cardJSON), &got); err != nil {
		t.Fatalf("decode card JSON: %v\n%s", err, cardJSON)
	}
	return got
}

func TestBuildProgressCardJSONFromPayload_NormalizesLegacyEntries(t *testing.T) {
	payload := &core.ProgressCardPayload{
		Entries: []string{
			"  ",
			"💭 checking plan",
			"🔧 Bash: go test ./platform/feishu",
			"🧾 ok",
			"❌ failed once",
			"plain update",
		},
		Agent:     "Codex",
		Lang:      string(core.LangChinese),
		State:     core.ProgressCardStateCompleted,
		Truncated: true,
	}

	card := decodeCardJSON(t, buildProgressCardJSONFromPayload(payload))
	header := card["header"].(map[string]any)
	if header["template"] != "green" {
		t.Fatalf("header template = %#v, want green", header["template"])
	}
	title := header["title"].(map[string]any)
	if title["content"] != "Codex · 已完成" {
		t.Fatalf("title = %#v, want completed zh title", title["content"])
	}

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal decoded card: %v", err)
	}
	rendered := string(raw)
	for _, want := range []string{"仅显示最近更新。", "思考 (1)", "工具 (2)", "更新 (2)", "checking plan", "Bash", "ok", "failed once", "plain update", "完整答复见下一条消息"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("progress card missing %q:\n%s", want, rendered)
		}
	}
}

func TestBuildProgressCardJSONFromPayload_ToolResultMetadataAndNoOutput(t *testing.T) {
	code := 2
	payload := &core.ProgressCardPayload{
		Items: []core.ProgressCardEntry{
			{Kind: core.ProgressEntryToolUse, Tool: "TodoWrite", Text: `{"todos":[{"content":"write tests","activeForm":"writing tests","status":"in_progress"}]}`},
			{Kind: core.ProgressEntryToolResult, Tool: "Bash", Text: "  ", ExitCode: &code},
			{Kind: core.ProgressEntryError, Text: "bad <input>"},
		},
		Agent: "  ",
		Lang:  string(core.LangEnglish),
		State: core.ProgressCardStateFailed,
	}

	cardJSON := buildProgressCardJSONFromPayload(payload)
	for _, want := range []string{"Agent · Failed", "Tools (2)", "Check", "todos", "🔴 exit code: `2`", "No output", "Error", "bad"} {
		if !strings.Contains(cardJSON, want) {
			t.Fatalf("progress card missing %q:\n%s", want, cardJSON)
		}
	}
	if strings.Contains(cardJSON, "<input>") {
		t.Fatalf("error text should be sanitized for card markdown, got %s", cardJSON)
	}
}

func TestBuildProgressCardJSONFromPayload_EmptyPayloadFallsBackToBlankCard(t *testing.T) {
	card := decodeCardJSON(t, buildProgressCardJSONFromPayload(&core.ProgressCardPayload{}))
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("elements = %#v, want one blank markdown element", elements)
	}
	markdown := elements[0].(map[string]any)
	if markdown["tag"] != "markdown" || markdown["content"] != " " {
		t.Fatalf("blank payload element = %#v, want blank markdown", markdown)
	}
}

func TestBuildCardJSONWithStatus_MapsHeaderTemplates(t *testing.T) {
	tests := []struct {
		status core.CardStatus
		want   string
	}{
		{core.CardStatusThinking, "blue"},
		{core.CardStatusWorking, "blue"},
		{core.CardStatusDone, "green"},
		{core.CardStatusError, "red"},
		{core.CardStatus("unknown"), "grey"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			card := decodeCardJSON(t, buildCardJSONWithStatus("content", tt.status))
			header := card["header"].(map[string]any)
			if header["template"] != tt.want {
				t.Fatalf("template = %#v, want %q", header["template"], tt.want)
			}
		})
	}
}

func TestBuildRichCard_CompactsOversizeStepsAndFallbackMarkdown(t *testing.T) {
	steps := make([]core.ToolStep, 0, 48)
	for i := 0; i < 24; i++ {
		steps = append(steps,
			core.ToolStep{Kind: core.ToolStepKindThinking, Summary: fmt.Sprintf("reasoning-%02d %s", i, strings.Repeat("r", 900))},
			core.ToolStep{Kind: core.ToolStepKindTool, Name: "Bash", Summary: fmt.Sprintf("command-%02d %s", i, strings.Repeat("c", 900)), Result: strings.Repeat("o", 900)},
		)
	}

	cardJSON := buildRichCard(core.CardStatusWorking, "", steps, "", true, "")
	if len(cardJSON) > maxRichCardJSONBytes {
		t.Fatalf("compacted rich card size = %d, want <= %d", len(cardJSON), maxRichCardJSONBytes)
	}
	if strings.Contains(cardJSON, "reasoning-00") || strings.Contains(cardJSON, "command-00") {
		t.Fatalf("compacted rich card should drop oldest steps:\n%s", cardJSON)
	}
	if !strings.Contains(cardJSON, "reasoning-23") && !strings.Contains(cardJSON, "command-23") {
		t.Fatalf("compacted rich card should keep latest activity:\n%s", cardJSON)
	}

	compact := compactRichStepsForCardSize(steps, 2, 18)
	if len(compact) != 4 {
		t.Fatalf("compactRichStepsForCardSize kept %d steps, want 4", len(compact))
	}
	if compact[0].Summary != "reasoning-22 rrrrr..." || compact[3].Summary != "command-23 ccccccc..." {
		t.Fatalf("compact step summaries = %#v", compact)
	}
	if compact[3].Result != "oooooooooooooooooo..." {
		t.Fatalf("compact result = %q, want truncated result", compact[3].Result)
	}

	fallback := compactRichFallbackMarkdown(steps)
	for _, want := range []string{"Card content is large", "reasoning-23", "command-23"} {
		if !strings.Contains(fallback, want) {
			t.Fatalf("fallback markdown missing %q:\n%s", want, fallback)
		}
	}
}

func TestSplitMarkdownByTables(t *testing.T) {
	md := strings.Join([]string{
		"intro",
		"| A |",
		"|---|",
		"| 1 |",
		"",
		"between",
		"",
		"| B |",
		"|---|",
		"| 2 |",
		"",
		"tail",
	}, "\n")

	parts := splitMarkdownByTables(md, 1)
	if len(parts) != 2 {
		t.Fatalf("parts = %#v, want first section plus overflow table", parts)
	}
	if !strings.Contains(parts[0], "intro") || !strings.Contains(parts[0], "| A |") {
		t.Fatalf("first part should keep intro and first table: %#v", parts)
	}
	if !strings.Contains(parts[1], "| B |") || strings.Contains(parts[1], "intro") {
		t.Fatalf("second part should contain only overflow table: %#v", parts)
	}

	if got := splitMarkdownByTables(md, 0); len(got) != 1 || got[0] != md {
		t.Fatalf("maxTables=0 parts = %#v, want original markdown", got)
	}
}
