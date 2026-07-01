package config_scenario

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/config"
	"github.com/chenhg5/cc-connect/core"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func baseProjectTOML(extra string) string {
	return `
data_dir = "` + filepath.ToSlash(os.TempDir()) + `/cc-connect-release-test"
` + extra + `

[[projects]]
name = "release"

[projects.agent]
type = "claudecode"
work_dir = "/tmp/cc-connect-release-work"

[[projects.platforms]]
type = "feishu"
app_id = "cli_release"
app_secret = "secret"
`
}

type configAgent struct {
	mu             sync.Mutex
	name           string
	opts           map[string]any
	providers      []core.ProviderConfig
	activeProvider string
	sessions       []*configSession
	records        []string
}

func newConfigAgent(name string, opts map[string]any) *configAgent {
	copied := make(map[string]any, len(opts))
	for k, v := range opts {
		copied[k] = v
	}
	return &configAgent{name: name, opts: copied}
}

func (a *configAgent) Name() string { return a.name }

func (a *configAgent) StartSession(_ context.Context, sessionID string) (core.AgentSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sessionID == "" {
		sessionID = "config-session"
	}
	session := &configSession{agent: a, id: sessionID, alive: true, events: make(chan core.Event, 8)}
	a.sessions = append(a.sessions, session)
	return session, nil
}

func (a *configAgent) ListSessions(context.Context) ([]core.AgentSessionInfo, error) {
	return nil, nil
}

func (a *configAgent) Stop() error {
	a.mu.Lock()
	sessions := append([]*configSession(nil), a.sessions...)
	a.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	return nil
}

func (a *configAgent) SetProviders(providers []core.ProviderConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.providers = append([]core.ProviderConfig(nil), providers...)
}

func (a *configAgent) SetActiveProvider(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, provider := range a.providers {
		if provider.Name == name {
			a.activeProvider = name
			return true
		}
	}
	return false
}

func (a *configAgent) GetActiveProvider() *core.ProviderConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, provider := range a.providers {
		if provider.Name == a.activeProvider {
			cp := provider
			return &cp
		}
	}
	return nil
}

func (a *configAgent) ListProviders() []core.ProviderConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]core.ProviderConfig(nil), a.providers...)
}

func (a *configAgent) record(prompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, prompt)
}

func (a *configAgent) waitRecord(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		if len(a.records) > 0 {
			prompt := a.records[0]
			a.mu.Unlock()
			return prompt
		}
		a.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Fatalf("timeout waiting for agent prompt, got %#v", a.records)
	return ""
}

type configSession struct {
	mu     sync.Mutex
	agent  *configAgent
	id     string
	alive  bool
	events chan core.Event
}

func (s *configSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	s.agent.record(prompt)
	s.events <- core.Event{Type: core.EventResult, Content: "config ok", Done: true}
	return nil
}

func (s *configSession) RespondPermission(string, core.PermissionResult) error { return nil }
func (s *configSession) Events() <-chan core.Event                             { return s.events }
func (s *configSession) CurrentSessionID() string                              { return s.id }
func (s *configSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}
func (s *configSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive {
		return nil
	}
	s.alive = false
	close(s.events)
	return nil
}

type configPlatform struct {
	mu      sync.Mutex
	name    string
	opts    map[string]any
	texts   []string
	images  []core.ImageAttachment
	files   []core.FileAttachment
	handler core.MessageHandler
	started bool
}

func newConfigPlatform(name string, opts map[string]any) *configPlatform {
	copied := make(map[string]any, len(opts))
	for k, v := range opts {
		copied[k] = v
	}
	return &configPlatform{name: name, opts: copied}
}

func (p *configPlatform) Name() string { return p.name }
func (p *configPlatform) Start(handler core.MessageHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = handler
	p.started = true
	return nil
}
func (p *configPlatform) Stop() error { return nil }
func (p *configPlatform) Reply(_ context.Context, replyCtx any, content string) error {
	return p.Send(context.Background(), replyCtx, content)
}
func (p *configPlatform) Send(_ context.Context, _ any, content string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, content)
	return nil
}
func (p *configPlatform) SendImage(_ context.Context, _ any, img core.ImageAttachment) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.images = append(p.images, img)
	return nil
}
func (p *configPlatform) SendFile(_ context.Context, _ any, file core.FileAttachment) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files = append(p.files, file)
	return nil
}
func (p *configPlatform) emit(t *testing.T, msg *core.Message) {
	t.Helper()
	p.mu.Lock()
	handler := p.handler
	started := p.started
	p.mu.Unlock()
	if handler == nil || !started {
		t.Fatalf("platform handler not ready: handler=%v started=%v", handler != nil, started)
	}
	handler(p, msg)
}
func (p *configPlatform) waitText(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		texts := append([]string(nil), p.texts...)
		p.mu.Unlock()
		for _, text := range texts {
			if strings.Contains(text, substr) {
				return text
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.Fatalf("timeout waiting for platform text containing %q, got %#v", substr, p.texts)
	return ""
}

type releaseLocalRuntime struct {
	cfg       *config.Config
	proj      *config.ProjectConfig
	agent     *configAgent
	platforms []*configPlatform
	engine    *core.Engine
}

func newReleaseLocalRuntime(t *testing.T, body string) *releaseLocalRuntime {
	t.Helper()
	cfg, err := config.Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(cfg.Projects))
	}
	proj := &cfg.Projects[0]
	agentOpts := map[string]any{}
	for k, v := range proj.Agent.Options {
		agentOpts[k] = v
	}
	agentOpts["cc_data_dir"] = cfg.DataDir
	agentOpts["cc_project"] = proj.Name
	agent := newConfigAgent(proj.Agent.Type, agentOpts)
	agent.SetProviders(configProvidersToCore(proj.Agent.Providers))
	if provider, _ := proj.Agent.Options["provider"].(string); provider != "" {
		if !agent.SetActiveProvider(provider) {
			t.Fatalf("configured provider %q was not available in %#v", provider, agent.providers)
		}
	}

	var platforms []*configPlatform
	var corePlatforms []core.Platform
	for _, pc := range proj.Platforms {
		opts := make(map[string]any, len(pc.Options)+2)
		for k, v := range pc.Options {
			opts[k] = v
		}
		opts["cc_data_dir"] = cfg.DataDir
		opts["cc_project"] = proj.Name
		platform := newConfigPlatform(pc.Type, opts)
		platforms = append(platforms, platform)
		corePlatforms = append(corePlatforms, platform)
	}
	engine := core.NewEngine(proj.Name, agent, corePlatforms, filepath.Join(t.TempDir(), "sessions.json"), core.LangEnglish)
	mode, tm, tool, thinkingMax, toolMax, showCtx, showFooter := config.EffectiveDisplay(cfg, proj)
	historyMax := config.EffectiveHistoryMaxLen(cfg, proj)
	engine.SetDisplayConfig(core.DisplayCfg{
		Mode:             mode,
		CardMode:         config.EffectiveCardMode(cfg, proj),
		ThinkingMessages: tm,
		ThinkingMaxLen:   thinkingMax,
		ToolMessages:     tool,
		ToolMaxLen:       toolMax,
		HistoryMaxLen:    &historyMax,
	})
	engine.SetShowContextIndicator(showCtx)
	engine.SetReplyFooterEnabled(showFooter)
	engine.SetAttachmentSendEnabled(cfg.AttachmentSend != "off")
	engine.SetInjectSender(proj.InjectSender == nil || *proj.InjectSender)
	t.Cleanup(func() {
		engine.Stop()
		_ = agent.Stop()
	})
	return &releaseLocalRuntime{cfg: cfg, proj: proj, agent: agent, platforms: platforms, engine: engine}
}

func configProvidersToCore(providers []config.ProviderConfig) []core.ProviderConfig {
	out := make([]core.ProviderConfig, len(providers))
	for i, provider := range providers {
		out[i] = core.ProviderConfig{
			Name:     provider.Name,
			APIKey:   provider.APIKey,
			BaseURL:  provider.BaseURL,
			Model:    provider.Model,
			Thinking: provider.Thinking,
			Env:      provider.Env,
		}
		for _, model := range provider.Models {
			out[i].Models = append(out[i].Models, core.ModelOption{Name: model.Model, Alias: model.Alias})
		}
		if provider.Codex != nil {
			out[i].CodexWireAPI = provider.Codex.WireAPI
			out[i].CodexHTTPHeaders = provider.Codex.HTTPHeaders
		}
	}
	return out
}

func configScenarioMessage(content string) *core.Message {
	return &core.Message{
		SessionKey: "fake:chat-1:user-1",
		Platform:   "fake",
		UserID:     "user-1",
		UserName:   "Release User",
		ChatName:   "Release Chat",
		Content:    content,
		ReplyCtx:   "reply-ctx",
	}
}

func TestReleaseConfig_ProjectDisplayOverridesGlobalFromLoadedConfig(t *testing.T) {
	path := writeConfig(t, `
attachment_send = "off"

[display]
mode = "quiet"
card_mode = "rich"
thinking_messages = true
tool_messages = false

[[projects]]
name = "release"
reset_on_idle_mins = 0

[projects.display]
mode = "full"
card_mode = "legacy"
thinking_messages = false
tool_messages = true
thinking_max_len = 111
tool_max_len = 222

[projects.agent]
type = "claudecode"
work_dir = "/tmp/cc-connect-release-work"

[[projects.platforms]]
type = "feishu"
app_id = "cli_release"
app_secret = "secret"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AttachmentSend != "off" {
		t.Fatalf("AttachmentSend = %q, want off", cfg.AttachmentSend)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(cfg.Projects))
	}
	proj := &cfg.Projects[0]
	if proj.ResetOnIdleMins == nil || *proj.ResetOnIdleMins != 0 {
		t.Fatalf("ResetOnIdleMins = %#v, want explicit 0", proj.ResetOnIdleMins)
	}

	mode, thinking, tools, thinkingMax, toolMax, _, _ := config.EffectiveDisplay(cfg, proj)
	if mode != config.DisplayModeFull {
		t.Fatalf("mode = %q, want project full override", mode)
	}
	if thinking {
		t.Fatal("thinking_messages = true, want project override false")
	}
	if !tools {
		t.Fatal("tool_messages = false, want project override true")
	}
	if thinkingMax != 111 || toolMax != 222 {
		t.Fatalf("max lens = %d/%d, want 111/222", thinkingMax, toolMax)
	}
	if got := config.EffectiveCardMode(cfg, proj); got != "legacy" {
		t.Fatalf("card mode = %q, want project legacy override", got)
	}
}

func TestReleaseConfig_InitializesFakeAgentAndPlatformsFromLoadedConfig(t *testing.T) {
	dataDir := filepath.ToSlash(t.TempDir())
	workDir := filepath.ToSlash(t.TempDir())
	runtime := newReleaseLocalRuntime(t, `
data_dir = "`+dataDir+`"

[[providers]]
name = "global-shared"
api_key = "sk-shared"
base_url = "https://shared.example.test"
model = "shared-model"
agent_types = ["fake-agent"]

[[projects]]
name = "release"
reset_on_idle_mins = 0

[projects.agent]
type = "fake-agent"

[projects.agent.options]
work_dir = "`+workDir+`"
provider = "project-primary"
mode = "bypassPermissions"

[[projects.agent.providers]]
name = "project-primary"
api_key = "sk-project"
base_url = "https://project.example.test"
model = "project-model"
thinking = "enabled"
[projects.agent.providers.env]
HTTPS_PROXY = "http://127.0.0.1:7890"

[[projects.platforms]]
type = "fake"
[projects.platforms.options]
app_id = "fake-app"
app_secret = "fake-secret"

[[projects.platforms]]
type = "audit"
[projects.platforms.options]
token = "audit-token"
`)

	if runtime.cfg.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want %q", runtime.cfg.DataDir, dataDir)
	}
	if got := runtime.agent.opts["cc_data_dir"]; got != dataDir {
		t.Fatalf("agent cc_data_dir = %#v, want %q", got, dataDir)
	}
	if got := runtime.agent.opts["cc_project"]; got != "release" {
		t.Fatalf("agent cc_project = %#v, want release", got)
	}
	if got := runtime.agent.opts["work_dir"]; got != workDir {
		t.Fatalf("agent work_dir = %#v, want %q", got, workDir)
	}
	if got := runtime.agent.opts["mode"]; got != "bypassPermissions" {
		t.Fatalf("agent mode option = %#v, want bypassPermissions", got)
	}
	active := runtime.agent.GetActiveProvider()
	if active == nil || active.Name != "project-primary" || active.Model != "project-model" || active.Env["HTTPS_PROXY"] == "" {
		t.Fatalf("active provider = %#v, want project-primary with model/env", active)
	}
	if len(runtime.platforms) != 2 {
		t.Fatalf("platform count = %d, want 2", len(runtime.platforms))
	}
	first := runtime.platforms[0]
	if first.Name() != "fake" || first.opts["cc_data_dir"] != dataDir || first.opts["cc_project"] != "release" || first.opts["app_id"] != "fake-app" {
		t.Fatalf("first platform = name:%s opts:%#v", first.Name(), first.opts)
	}
	second := runtime.platforms[1]
	if second.Name() != "audit" || second.opts["token"] != "audit-token" || second.opts["cc_project"] != "release" {
		t.Fatalf("second platform = name:%s opts:%#v", second.Name(), second.opts)
	}
}

func TestReleaseConfig_EngineUsesConfigSwitchesWithFakeRuntime(t *testing.T) {
	dataDir := filepath.ToSlash(t.TempDir())
	workDir := filepath.ToSlash(t.TempDir())
	runtime := newReleaseLocalRuntime(t, `
data_dir = "`+dataDir+`"
attachment_send = "off"

[[projects]]
name = "release"
inject_sender = true
reset_on_idle_mins = 0

[projects.agent]
type = "fake-agent"

[projects.agent.options]
work_dir = "`+workDir+`"

[[projects.platforms]]
type = "fake"
[projects.platforms.options]
app_id = "fake-app"
app_secret = "fake-secret"
`)

	if err := runtime.engine.Start(); err != nil {
		t.Fatalf("engine.Start() error = %v", err)
	}
	platform := runtime.platforms[0]
	platform.emit(t, configScenarioMessage("check initialized config"))

	prompt := runtime.agent.waitRecord(t)
	if !strings.Contains(prompt, `[cc-connect sender_id=user-1 sender_name="Release User" platform=fake chat_id=chat-1]`) {
		t.Fatalf("prompt = %q, want injected sender header", prompt)
	}
	if !strings.Contains(prompt, "check initialized config") {
		t.Fatalf("prompt = %q, want message content", prompt)
	}
	platform.waitText(t, "config ok")

	err := runtime.engine.SendToSessionWithAttachments(
		"fake:chat-1:user-1",
		"generated attachment",
		[]core.ImageAttachment{{FileName: "image.png", MimeType: "image/png", Data: []byte("png")}},
		nil,
		nil,
		false,
	)
	if !errors.Is(err, core.ErrAttachmentSendDisabled) {
		t.Fatalf("SendToSessionWithAttachments() error = %v, want ErrAttachmentSendDisabled", err)
	}
}

func TestReleaseConfig_InjectSenderDefaultsEnabledWithFakeRuntime(t *testing.T) {
	runtime := newReleaseLocalRuntime(t, `
data_dir = "`+filepath.ToSlash(t.TempDir())+`"

[[projects]]
name = "release"

[projects.agent]
type = "fake-agent"

[[projects.platforms]]
type = "fake"
[projects.platforms.options]
app_id = "fake-app"
app_secret = "fake-secret"
`)

	if err := runtime.engine.Start(); err != nil {
		t.Fatalf("engine.Start() error = %v", err)
	}
	platform := runtime.platforms[0]
	platform.emit(t, configScenarioMessage("default sender injection"))

	prompt := runtime.agent.waitRecord(t)
	if !strings.Contains(prompt, `[cc-connect sender_id=user-1 sender_name="Release User" platform=fake chat_id=chat-1]`) {
		t.Fatalf("prompt = %q, want default injected sender header", prompt)
	}
	platform.waitText(t, "config ok")
}

func TestReleaseConfig_InjectSenderExplicitFalseDisablesFakeRuntime(t *testing.T) {
	runtime := newReleaseLocalRuntime(t, `
data_dir = "`+filepath.ToSlash(t.TempDir())+`"

[[projects]]
name = "release"
inject_sender = false

[projects.agent]
type = "fake-agent"

[[projects.platforms]]
type = "fake"
[projects.platforms.options]
app_id = "fake-app"
app_secret = "fake-secret"
`)

	if err := runtime.engine.Start(); err != nil {
		t.Fatalf("engine.Start() error = %v", err)
	}
	platform := runtime.platforms[0]
	platform.emit(t, configScenarioMessage("disabled sender injection"))

	prompt := runtime.agent.waitRecord(t)
	if strings.Contains(prompt, "[cc-connect sender_id=") {
		t.Fatalf("prompt = %q, want no injected sender header", prompt)
	}
	if !strings.Contains(prompt, "disabled sender injection") {
		t.Fatalf("prompt = %q, want message content", prompt)
	}
	platform.waitText(t, "config ok")
}

func TestReleaseConfig_DefaultsKeepAttachmentsAndFullDisplayEnabled(t *testing.T) {
	path := writeConfig(t, baseProjectTOML(""))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AttachmentSend != "on" {
		t.Fatalf("AttachmentSend = %q, want default on", cfg.AttachmentSend)
	}
	mode, thinking, tools, _, _, _, _ := config.EffectiveDisplay(cfg, &cfg.Projects[0])
	if mode != config.DisplayModeFull || !thinking || !tools {
		t.Fatalf("display = mode:%s thinking:%v tools:%v, want full/true/true", mode, thinking, tools)
	}
	if got := config.EffectiveCardMode(cfg, &cfg.Projects[0]); got != "legacy" {
		t.Fatalf("card mode = %q, want default legacy", got)
	}
}

func TestReleaseConfig_BehaviorControlSwitchesParseFromLoadedConfig(t *testing.T) {
	path := writeConfig(t, `
[stream_preview]
enabled = false
disabled_platforms = ["feishu", "feishu"]
interval_ms = 250
min_delta_chars = 12
max_chars = 777

[[projects]]
name = "release"
show_context_indicator = false
reply_footer = false
disabled_commands = ["restart", "shell"]

[projects.display]
mode = "quiet"
card_mode = "rich"
thinking_messages = false
tool_messages = false

[projects.agent]
type = "claudecode"
work_dir = "/tmp/cc-connect-release-work"

[[projects.platforms]]
type = "feishu"
app_id = "cli_release"
app_secret = "secret"
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StreamPreview.Enabled == nil || *cfg.StreamPreview.Enabled {
		t.Fatalf("stream_preview.enabled = %#v, want false", cfg.StreamPreview.Enabled)
	}
	if got := strings.Join(cfg.StreamPreview.DisabledPlatforms, ","); got != "feishu,feishu" {
		t.Fatalf("stream_preview.disabled_platforms = %#v", cfg.StreamPreview.DisabledPlatforms)
	}
	if cfg.StreamPreview.IntervalMs == nil || *cfg.StreamPreview.IntervalMs != 250 {
		t.Fatalf("stream_preview.interval_ms = %#v, want 250", cfg.StreamPreview.IntervalMs)
	}
	if cfg.StreamPreview.MinDeltaChars == nil || *cfg.StreamPreview.MinDeltaChars != 12 {
		t.Fatalf("stream_preview.min_delta_chars = %#v, want 12", cfg.StreamPreview.MinDeltaChars)
	}
	if cfg.StreamPreview.MaxChars == nil || *cfg.StreamPreview.MaxChars != 777 {
		t.Fatalf("stream_preview.max_chars = %#v, want 777", cfg.StreamPreview.MaxChars)
	}

	proj := &cfg.Projects[0]
	if proj.ShowContextIndicator == nil || *proj.ShowContextIndicator {
		t.Fatalf("show_context_indicator = %#v, want false", proj.ShowContextIndicator)
	}
	if proj.ReplyFooter == nil || *proj.ReplyFooter {
		t.Fatalf("reply_footer = %#v, want false", proj.ReplyFooter)
	}
	if strings.Join(proj.DisabledCommands, ",") != "restart,shell" {
		t.Fatalf("disabled_commands = %#v", proj.DisabledCommands)
	}
	mode, thinking, tools, _, _, _, _ := config.EffectiveDisplay(cfg, proj)
	if mode != config.DisplayModeQuiet || thinking || tools {
		t.Fatalf("display = mode:%s thinking:%v tools:%v, want quiet/false/false", mode, thinking, tools)
	}
	if got := config.EffectiveCardMode(cfg, proj); got != "rich" {
		t.Fatalf("card mode = %q, want rich", got)
	}
}

func TestReleaseConfig_InvalidCriticalOptionsFailFast(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{
			name: "invalid attachment send",
			toml: baseProjectTOML(`
attachment_send = "maybe"
`),
			wantErr: `attachment_send must be "on" or "off"`,
		},
		{
			name: "invalid project display mode",
			toml: `
[[projects]]
name = "release"

[projects.display]
mode = "verbose"

[projects.agent]
type = "claudecode"
work_dir = "/tmp/cc-connect-release-work"

[[projects.platforms]]
type = "feishu"
app_id = "cli_release"
app_secret = "secret"
`,
			wantErr: `projects[0].display.mode must be "full", "compact", or "quiet"`,
		},
		{
			name: "negative reset on idle",
			toml: `
[[projects]]
name = "release"
reset_on_idle_mins = -1

[projects.agent]
type = "claudecode"
work_dir = "/tmp/cc-connect-release-work"

[[projects.platforms]]
type = "feishu"
app_id = "cli_release"
app_secret = "secret"
`,
			wantErr: "reset_on_idle_mins must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(writeConfig(t, tt.toml))
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %q, want contains %q", err.Error(), tt.wantErr)
			}
		})
	}
}
