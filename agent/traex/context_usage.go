package traex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

const traexRolloutTailBytes int64 = 1 << 20
const traexContextBaselineTokens = 12000

type traexSnakeTokenUsage struct {
	TotalTokens           int `json:"total_tokens"`
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

func cloneContextUsage(usage *core.ContextUsage) *core.ContextUsage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	return &cloned
}

func loadContextUsageFromRollout(sessionID, cachedPath string) (*core.ContextUsage, string, error) {
	path := strings.TrimSpace(cachedPath)
	if path != "" {
		usage, err := readContextUsageFromRollout(path)
		if err == nil && usage != nil {
			return usage, path, nil
		}
	}

	traeHome := resolveTraeHomeDir()
	path = findSessionFileInTraeHome(traeHome, sessionID)
	if path == "" {
		return nil, "", fmt.Errorf("session file not found for %s", sessionID)
	}
	usage, err := readContextUsageFromRollout(path)
	if err != nil {
		return nil, path, err
	}
	if usage == nil {
		return nil, path, fmt.Errorf("context usage not found in rollout")
	}
	return usage, path, nil
}

func resolveTraeHomeDir() string {
	if value := strings.TrimSpace(os.Getenv("TRAE_HOME")); value != "" {
		return value
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".trae")
}

func findSessionFileInTraeHome(traeHome, sessionID string) string {
	if strings.TrimSpace(traeHome) == "" || strings.TrimSpace(sessionID) == "" {
		return ""
	}

	// traex sessions are at ~/.trae/cli/sessions/<year>/<month>/<day>/rollout-*.jsonl
	sessionsDir := filepath.Join(traeHome, "cli", "sessions")

	pattern := filepath.Join(sessionsDir, "*", "*", "*", "rollout-*"+sessionID+".jsonl")
	if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
		sort.Strings(matches)
		return matches[len(matches)-1]
	}

	var found string
	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found != "" {
			return nil
		}
		if strings.Contains(filepath.Base(path), sessionID) {
			found = path
		}
		return nil
	})
	return found
}

func readContextUsageFromRollout(path string) (*core.ContextUsage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if usage, err := readContextUsageFromRolloutTail(f); err != nil {
		return nil, err
	} else if usage != nil {
		return usage, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return scanContextUsageFromRollout(f)
}

func readContextUsageFromRolloutTail(f *os.File) (*core.ContextUsage, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 {
		return nil, nil
	}

	start := int64(0)
	if info.Size() > traexRolloutTailBytes {
		start = info.Size() - traexRolloutTailBytes
	}
	buf := make([]byte, int(info.Size()-start))
	n, err := f.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]
	if start > 0 {
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
	}
	return parseContextUsageFromRolloutBytes(buf), nil
}

func parseContextUsageFromRolloutBytes(data []byte) *core.ContextUsage {
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		if usage := parseContextUsageFromRolloutLine(line); usage != nil {
			return usage
		}
	}
	return nil
}

func scanContextUsageFromRollout(r io.Reader) (*core.ContextUsage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)

	var last *core.ContextUsage
	for scanner.Scan() {
		if usage := parseContextUsageFromRolloutLine(scanner.Bytes()); usage != nil {
			last = usage
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return last, nil
}

func parseContextUsageFromRolloutLine(line []byte) *core.ContextUsage {
	var entry struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(line, &entry); err != nil {
		return nil
	}
	if entry.Type != "event_msg" {
		return nil
	}

	var payload struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage    traexSnakeTokenUsage `json:"total_token_usage"`
			LastTokenUsage     traexSnakeTokenUsage `json:"last_token_usage"`
			ModelContextWindow int                  `json:"model_context_window"`
		} `json:"info"`
	}
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return nil
	}
	if payload.Type != "token_count" || payload.Info == nil {
		return nil
	}
	return contextUsageFromSnake(payload.Info.LastTokenUsage, payload.Info.ModelContextWindow)
}

func contextUsageFromSnake(usage traexSnakeTokenUsage, contextWindow int) *core.ContextUsage {
	usedTokens := currentContextTokens(usage.TotalTokens, usage.InputTokens, usage.OutputTokens)
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return nil
	}
	if contextWindow <= 0 {
		return nil
	}
	return &core.ContextUsage{
		UsedTokens:            usedTokens,
		BaselineTokens:        traexContextBaselineTokens,
		TotalTokens:           usage.TotalTokens,
		InputTokens:           usage.InputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		OutputTokens:          usage.OutputTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens,
		ContextWindow:         contextWindow,
	}
}

func currentContextTokens(totalTokens, inputTokens, outputTokens int) int {
	if totalTokens > 0 {
		return totalTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		return inputTokens + outputTokens
	}
	return 0
}
