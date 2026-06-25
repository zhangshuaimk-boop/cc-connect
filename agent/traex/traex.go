package traex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/chenhg5/cc-connect/core"
)

func init() {
	core.RegisterAgent("traex", New)
	// traecli 是 TraeX 的品牌名（二进制 --version 输出 "traecli x.y.z"），
	// 用户常按品牌名配置引擎；注册成别名让 engine 也认它。
	core.RegisterAgent("traecli", New)
}

// Agent drives Trae CLI (traex/traecli) using `traex exec --json`.
//
// Modes (maps to traex exec flags):
//   - "default":   normal permissions (ask permission for tools)
//   - "plan":      --permission-mode plan (read-only analysis)
//   - "auto-edit": --permission-mode bypass_permissions (auto-approve)
//   - "full-auto": --permission-mode bypass_permissions
//   - "yolo":      --dangerously-bypass-approvals-and-sandbox
type Agent struct {
	workDir         string
	model           string
	reasoningEffort string
	mode            string
	cliBin          string   // CLI binary name, default "traex"
	cliExtraArgs    []string // extra args parsed from cmd after the binary
	providers       []core.ProviderConfig
	activeIdx       int      // -1 = no provider set
	configEnv       []string // env vars from [projects.agent.options.env]
	sessionEnv      []string
	mu              sync.RWMutex
}

func New(opts map[string]any) (core.Agent, error) {
	workDir, _ := opts["work_dir"].(string)
	if workDir == "" {
		workDir = "."
	}
	model, _ := opts["model"].(string)
	reasoningEffort, _ := opts["reasoning_effort"].(string)
	mode, _ := opts["mode"].(string)
	mode = normalizeMode(mode)

	cliBin, cliExtraArgs := core.ParseCmdOpts(opts, "traex")

	if _, err := exec.LookPath(cliBin); err != nil {
		return nil, fmt.Errorf("traex: %q CLI not found in PATH", cliBin)
	}

	return &Agent{
		workDir:         workDir,
		model:           model,
		reasoningEffort: normalizeReasoningEffort(reasoningEffort),
		mode:            mode,
		cliBin:          cliBin,
		cliExtraArgs:    cliExtraArgs,
		configEnv:       core.ParseConfigEnv(opts),
		activeIdx:       -1,
	}, nil
}

func normalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto-edit", "autoedit", "auto_edit", "edit":
		return "auto-edit"
	case "full-auto", "fullauto", "full_auto", "auto":
		return "full-auto"
	case "yolo", "bypass", "dangerously-bypass":
		return "yolo"
	case "plan":
		return "plan"
	case "ask":
		return "ask"
	default:
		return "default"
	}
}

func normalizeReasoningEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "low":
		return "low"
	case "medium", "med":
		return "medium"
	case "high":
		return "high"
	case "xhigh", "x-high", "very-high":
		return "xhigh"
	default:
		return ""
	}
}

func (a *Agent) Name() string { return "traex" }

func (a *Agent) SetWorkDir(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.workDir = dir
	slog.Info("traex: work_dir changed", "work_dir", dir)
}

func (a *Agent) GetWorkDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.workDir
}

func (a *Agent) SetModel(model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.model = model
	slog.Info("traex: model changed", "model", model)
}

func (a *Agent) GetModel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return core.GetProviderModel(a.providers, a.activeIdx, a.model)
}

func (a *Agent) SetReasoningEffort(effort string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reasoningEffort = normalizeReasoningEffort(effort)
	slog.Info("traex: reasoning effort changed", "reasoning_effort", a.reasoningEffort)
}

func (a *Agent) GetReasoningEffort() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reasoningEffort
}

func (a *Agent) AvailableReasoningEfforts() []string {
	return []string{"low", "medium", "high", "xhigh"}
}

func (a *Agent) configuredModels() []core.ModelOption {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return core.GetProviderModels(a.providers, a.activeIdx)
}

func (a *Agent) AvailableModels(ctx context.Context) []core.ModelOption {
	if models := a.configuredModels(); len(models) > 0 {
		return models
	}
	return nil
}

func (a *Agent) SetSessionEnv(env []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionEnv = env
}

func (a *Agent) StartSession(ctx context.Context, sessionID string) (core.AgentSession, error) {
	a.mu.Lock()
	mode := a.mode
	model := a.model
	reasoningEffort := a.reasoningEffort
	cliBin := a.cliBin
	cliExtraArgs := a.cliExtraArgs
	workDir := a.workDir
	extraEnv := append([]string(nil), a.configEnv...)
	extraEnv = append(extraEnv, a.providerEnvLocked()...)
	extraEnv = append(extraEnv, a.sessionEnv...)
	var baseURL string
	var provName string
	if a.activeIdx >= 0 && a.activeIdx < len(a.providers) {
		if m := a.providers[a.activeIdx].Model; m != "" {
			model = m
		}
		baseURL = a.providers[a.activeIdx].BaseURL
		provName = a.providers[a.activeIdx].Name
	}
	a.mu.Unlock()

	return newTraexSession(ctx, cliBin, cliExtraArgs, workDir, model, reasoningEffort, mode, sessionID, baseURL, extraEnv, provName)
}

func (a *Agent) ListSessions(_ context.Context) ([]core.AgentSessionInfo, error) {
	a.mu.RLock()
	workDir := a.workDir
	a.mu.RUnlock()
	return listTraexSessions(workDir)
}

func (a *Agent) GetSessionHistory(_ context.Context, sessionID string, limit int) ([]core.HistoryEntry, error) {
	return getSessionHistory(sessionID, limit)
}

func (a *Agent) DeleteSession(_ context.Context, sessionID string) error {
	path := findSessionFile(sessionID)
	if path == "" {
		return fmt.Errorf("session file not found: %s", sessionID)
	}
	return os.Remove(path)
}

func (a *Agent) Stop() error { return nil }

func (a *Agent) SetMode(mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = normalizeMode(mode)
	slog.Info("traex: approval mode changed", "mode", a.mode)
}

func (a *Agent) GetMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

func (a *Agent) WorkspaceAgentOptions() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	opts := map[string]any{
		"mode": a.mode,
	}
	if a.model != "" {
		opts["model"] = a.model
	}
	if a.reasoningEffort != "" {
		opts["reasoning_effort"] = a.reasoningEffort
	}
	return opts
}

// ── SkillProvider implementation ──────────────────────────────

func (a *Agent) SkillDirs() []string {
	a.mu.RLock()
	workDir := a.workDir
	a.mu.RUnlock()
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		absDir = workDir
	}
	return traexSkillDirs(absDir)
}

func traexSkillDirs(workDir string) []string {
	homeDir, _ := os.UserHomeDir()
	traeHome := resolveTraeHomeDir()

	projectDirs := walkUpTraexProjectSkillDirs(workDir, homeDir)
	userDirs := make([]string, 0, 2)
	if traeHome != "" {
		userDirs = append(userDirs, filepath.Join(traeHome, "skills"))
	}
	if homeDir != "" {
		userDirs = append(userDirs, filepath.Join(homeDir, ".agents", "skills"))
	}
	return uniqueTraexSkillDirs(append(projectDirs, userDirs...))
}

func walkUpTraexProjectSkillDirs(workDir, homeDir string) []string {
	current := filepath.Clean(workDir)
	homeDir = filepath.Clean(homeDir)
	stopAt := findTraexProjectRoot(current)

	var dirs []string
	for {
		if homeDir != "" && sameTraexPath(current, homeDir) {
			break
		}
		dirs = append(dirs,
			filepath.Join(current, ".agents", "skills"),
			filepath.Join(current, ".trae", "skills"),
		)
		if stopAt != "" && sameTraexPath(current, stopAt) {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return uniqueTraexSkillDirs(dirs)
}

func findTraexProjectRoot(start string) string {
	current := filepath.Clean(start)
	for {
		for _, marker := range []string{".git", ".jj"} {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func sameTraexPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func uniqueTraexSkillDirs(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

// ── MemoryFileProvider implementation ─────────────────────────

func (a *Agent) ProjectMemoryFile() string {
	absDir, err := filepath.Abs(a.workDir)
	if err != nil {
		absDir = a.workDir
	}
	return filepath.Join(absDir, "AGENTS.md")
}

func (a *Agent) GlobalMemoryFile() string {
	traeHome := resolveTraeHomeDir()
	if traeHome == "" {
		return ""
	}
	return filepath.Join(traeHome, "AGENTS.md")
}

// ── ContextCompressor implementation ──────────────────────────

func (a *Agent) CompressCommand() string { return "" }

// ── ProviderSwitcher implementation ──────────────────────────

func (a *Agent) SetProviders(providers []core.ProviderConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.providers = providers
}

func (a *Agent) SetActiveProvider(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "" {
		a.activeIdx = -1
		slog.Info("traex: provider cleared")
		return true
	}
	for i, p := range a.providers {
		if p.Name == name {
			a.activeIdx = i
			slog.Info("traex: provider switched", "provider", name)
			return true
		}
	}
	return false
}

func (a *Agent) GetActiveProvider() *core.ProviderConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeIdx < 0 || a.activeIdx >= len(a.providers) {
		return nil
	}
	p := a.providers[a.activeIdx]
	return &p
}

func (a *Agent) ListProviders() []core.ProviderConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]core.ProviderConfig, len(a.providers))
	copy(result, a.providers)
	return result
}

func (a *Agent) providerEnvLocked() []string {
	if a.activeIdx < 0 || a.activeIdx >= len(a.providers) {
		return nil
	}
	p := a.providers[a.activeIdx]
	var env []string
	if p.APIKey != "" {
		env = append(env, "OPENAI_API_KEY="+p.APIKey)
	}
	if p.BaseURL != "" {
		env = append(env, "OPENAI_BASE_URL="+p.BaseURL)
	}
	for k, v := range p.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// ── PermissionModeProvider implementation ─────────────────────

func (a *Agent) PermissionModes() []core.PermissionModeInfo {
	return []core.PermissionModeInfo{
		{Key: "default", Name: "Default", NameZh: "默认", Desc: "Ask permission for every tool call", DescZh: "每次工具调用都需确认"},
		{Key: "plan", Name: "Plan", NameZh: "计划", Desc: "Read-only analysis mode", DescZh: "只读分析模式"},
		{Key: "auto-edit", Name: "Auto Edit", NameZh: "自动编辑", Desc: "Auto-approve within workspace", DescZh: "工作区内自动通过"},
		{Key: "full-auto", Name: "Full Auto", NameZh: "全自动", Desc: "Auto-approve with sandbox", DescZh: "自动通过（沙箱）"},
		{Key: "yolo", Name: "YOLO", NameZh: "YOLO 模式", Desc: "Bypass all approvals and sandbox", DescZh: "跳过所有审批和沙箱"},
	}
}
