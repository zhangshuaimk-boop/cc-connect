package engine_scenario

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

type promptRecord struct {
	sessionID string
	prompt    string
}

type scenarioAgent struct {
	mu       sync.Mutex
	sessions []*scenarioSession
	list     []core.AgentSessionInfo
	records  []promptRecord
}

func newScenarioAgent() *scenarioAgent {
	return &scenarioAgent{}
}

func (a *scenarioAgent) Name() string { return "scenario-agent" }

func (a *scenarioAgent) StartSession(_ context.Context, sessionID string) (core.AgentSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sessionID == "" {
		sessionID = fmt.Sprintf("agent-session-%d", len(a.sessions)+1)
	}
	session := &scenarioSession{agent: a, id: sessionID, alive: true, events: make(chan core.Event, 32)}
	a.sessions = append(a.sessions, session)
	a.list = append(a.list, core.AgentSessionInfo{
		ID:           sessionID,
		Summary:      "release scenario session",
		MessageCount: 1,
		ModifiedAt:   time.Now(),
	})
	return session, nil
}

func (a *scenarioAgent) ListSessions(_ context.Context) ([]core.AgentSessionInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]core.AgentSessionInfo(nil), a.list...), nil
}

func (a *scenarioAgent) Stop() error {
	a.mu.Lock()
	sessions := append([]*scenarioSession(nil), a.sessions...)
	a.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	return nil
}

func (a *scenarioAgent) addRecord(sessionID, prompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, promptRecord{sessionID: sessionID, prompt: prompt})
}

func (a *scenarioAgent) waitRecords(t *testing.T, n int) []promptRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		if len(a.records) >= n {
			out := append([]promptRecord(nil), a.records...)
			a.mu.Unlock()
			return out
		}
		a.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Fatalf("timeout waiting for %d prompts, got %d: %#v", n, len(a.records), a.records)
	return nil
}

func (a *scenarioAgent) recordCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.records)
}

type scenarioSession struct {
	mu      sync.Mutex
	agent   *scenarioAgent
	id      string
	alive   bool
	events  chan core.Event
	counter int
}

func (s *scenarioSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	s.mu.Lock()
	id := s.id
	s.counter++
	count := s.counter
	s.mu.Unlock()

	s.agent.addRecord(id, prompt)
	s.events <- core.Event{Type: core.EventResult, Content: "scenario response " + id + " #" + strconv.Itoa(count), Done: true}
	return nil
}

func (s *scenarioSession) Events() <-chan core.Event { return s.events }
func (s *scenarioSession) RespondPermission(string, core.PermissionResult) error {
	return nil
}
func (s *scenarioSession) CurrentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}
func (s *scenarioSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}
func (s *scenarioSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alive {
		return nil
	}
	s.alive = false
	close(s.events)
	return nil
}

type scenarioPlatform struct {
	mu    sync.Mutex
	texts []string
}

func (p *scenarioPlatform) Name() string { return "scenario" }
func (p *scenarioPlatform) Start(core.MessageHandler) error {
	return nil
}
func (p *scenarioPlatform) Stop() error { return nil }
func (p *scenarioPlatform) Reply(_ context.Context, replyCtx any, content string) error {
	return p.Send(context.Background(), replyCtx, content)
}
func (p *scenarioPlatform) Send(_ context.Context, _ any, content string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = append(p.texts, content)
	return nil
}
func (p *scenarioPlatform) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.texts = nil
}
func (p *scenarioPlatform) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.texts...)
}
func (p *scenarioPlatform) waitTextContaining(t *testing.T, substr string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, text := range p.snapshot() {
			if strings.Contains(strings.ToLower(text), strings.ToLower(substr)) {
				return text
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q, got %#v", substr, p.snapshot())
	return ""
}

func newScenarioEngine(t *testing.T) (*core.Engine, *scenarioAgent, *scenarioPlatform) {
	t.Helper()
	agent := newScenarioAgent()
	platform := &scenarioPlatform{}
	engine := core.NewEngine("release-core", agent, []core.Platform{platform}, t.TempDir()+"/sessions.json", core.LangEnglish)
	t.Cleanup(func() {
		engine.Stop()
		_ = agent.Stop()
	})
	return engine, agent, platform
}

func scenarioMessage(content string) *core.Message {
	return &core.Message{
		SessionKey: "scenario:chat-1:user-1",
		Platform:   "scenario",
		UserID:     "user-1",
		UserName:   "Release Tester",
		ChatName:   "Release Room",
		Content:    content,
		ReplyCtx:   "reply-ctx",
	}
}

func receive(engine *core.Engine, platform *scenarioPlatform, content string) {
	engine.ReceiveMessage(platform, scenarioMessage(content))
}

func TestSessionLifecycleCommandsThroughReceiveMessage(t *testing.T) {
	engine, agent, platform := newScenarioEngine(t)

	receive(engine, platform, "first user turn")
	agent.waitRecords(t, 1)
	platform.waitTextContaining(t, "scenario response")
	platform.clear()

	receive(engine, platform, "/new release-named")
	platform.waitTextContaining(t, "release-named")
	platform.clear()

	receive(engine, platform, "second user turn")
	agent.waitRecords(t, 2)
	platform.waitTextContaining(t, "scenario response")
	platform.clear()

	receive(engine, platform, "/list")
	platform.waitTextContaining(t, "release-named")
	platform.clear()

	receive(engine, platform, "/name renamed-release")
	platform.waitTextContaining(t, "renamed-release")
	platform.clear()

	receive(engine, platform, "/list")
	platform.waitTextContaining(t, "renamed-release")
	platform.clear()

	receive(engine, platform, "/current")
	platform.waitTextContaining(t, "session")
	platform.clear()

	receive(engine, platform, "/status")
	platform.waitTextContaining(t, "user-1")
}

func TestAliasDisabledCommandAndBannedWordsThroughReceiveMessage(t *testing.T) {
	engine, agent, platform := newScenarioEngine(t)
	engine.AddAlias("帮助", "/whoami")

	receive(engine, platform, "帮助")
	platform.waitTextContaining(t, "user-1")
	if got := agent.recordCount(); got != 0 {
		t.Fatalf("alias to command should not reach agent, got %d prompts", got)
	}
	platform.clear()

	engine.SetDisabledCommands([]string{"whoami"})
	receive(engine, platform, "/whoami")
	platform.waitTextContaining(t, "disabled")
	if got := agent.recordCount(); got != 0 {
		t.Fatalf("disabled command should not reach agent, got %d prompts", got)
	}
	platform.clear()

	engine.SetBannedWords([]string{"forbidden"})
	receive(engine, platform, "this contains forbidden content")
	platform.waitTextContaining(t, "blocked")
	if got := agent.recordCount(); got != 0 {
		t.Fatalf("banned message should not reach agent, got %d prompts", got)
	}
}

func TestCustomPromptCommandThroughReceiveMessage(t *testing.T) {
	engine, agent, platform := newScenarioEngine(t)
	engine.AddCommand("daily", "Daily summary", "Summarize release status for {{1}}", "", "", "release-test")

	receive(engine, platform, "/daily beta")

	records := agent.waitRecords(t, 1)
	if !strings.Contains(records[0].prompt, "Summarize release status for beta") {
		t.Fatalf("custom command prompt = %q", records[0].prompt)
	}
	platform.waitTextContaining(t, "scenario response")
}

func TestUnknownSlashCommandNotifiesThenFallsThroughToAgent(t *testing.T) {
	engine, agent, platform := newScenarioEngine(t)

	receive(engine, platform, "/not-a-command keep this request")

	platform.waitTextContaining(t, "forwarding")
	records := agent.waitRecords(t, 1)
	if !strings.Contains(records[0].prompt, "/not-a-command keep this request") {
		t.Fatalf("unknown slash command should fall through to agent, got prompt %q", records[0].prompt)
	}
}
