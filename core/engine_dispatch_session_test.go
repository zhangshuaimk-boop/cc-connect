package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type dispatchRecordingSession struct {
	id        string
	events    chan Event
	mu        sync.Mutex
	prompts   []string
	alive     bool
	closeOnce sync.Once
}

func newDispatchRecordingSession(id string) *dispatchRecordingSession {
	return &dispatchRecordingSession{
		id:     id,
		events: make(chan Event, 4),
		alive:  true,
	}
}

func (s *dispatchRecordingSession) Send(prompt string, _ []ImageAttachment, _ []FileAttachment) error {
	s.mu.Lock()
	s.prompts = append(s.prompts, prompt)
	s.mu.Unlock()
	s.events <- Event{Type: EventResult, Content: "done:" + prompt, Done: true}
	return nil
}

func (s *dispatchRecordingSession) RespondPermission(string, PermissionResult) error { return nil }
func (s *dispatchRecordingSession) Events() <-chan Event                             { return s.events }
func (s *dispatchRecordingSession) CurrentSessionID() string                         { return s.id }
func (s *dispatchRecordingSession) Alive() bool                                      { return s.alive }
func (s *dispatchRecordingSession) Close() error {
	s.closeOnce.Do(func() {
		s.alive = false
		close(s.events)
	})
	return nil
}

func (s *dispatchRecordingSession) sentPrompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.prompts))
	copy(out, s.prompts)
	return out
}

type dispatchRecordingAgent struct {
	name     string
	workDir  string
	validate func(context.Context, string) bool

	mu          sync.Mutex
	startedWith []string
	next        []*dispatchRecordingSession
	startErr    error
}

func (a *dispatchRecordingAgent) Name() string {
	if a.name == "" {
		return "dispatch-recorder"
	}
	return a.name
}

func (a *dispatchRecordingAgent) StartSession(ctx context.Context, sessionID string) (AgentSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startedWith = append(a.startedWith, sessionID)
	if a.startErr != nil {
		return nil, a.startErr
	}
	if len(a.next) > 0 {
		s := a.next[0]
		a.next = a.next[1:]
		return s, nil
	}
	return newDispatchRecordingSession("dispatch-auto"), nil
}

func (a *dispatchRecordingAgent) ListSessions(context.Context) ([]AgentSessionInfo, error) {
	return nil, nil
}

func (a *dispatchRecordingAgent) Stop() error { return nil }

func (a *dispatchRecordingAgent) ValidateSessionID(ctx context.Context, sessionID string) bool {
	if a.validate == nil {
		return true
	}
	return a.validate(ctx, sessionID)
}

func (a *dispatchRecordingAgent) SetWorkDir(dir string) { a.workDir = dir }
func (a *dispatchRecordingAgent) GetWorkDir() string    { return a.workDir }

func (a *dispatchRecordingAgent) starts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.startedWith))
	copy(out, a.startedWith)
	return out
}

func waitForDispatchSent(t *testing.T, p *stubPlatformEngine, n int) []string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		sent := p.getSent()
		if len(sent) >= n {
			return sent
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d platform sends, got %#v", n, sent)
		case <-ticker.C:
		}
	}
}

func waitForDispatchPrompts(t *testing.T, s *dispatchRecordingSession, n int) []string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		prompts := s.sentPrompts()
		if len(prompts) >= n {
			return prompts
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d prompts, got %#v", n, prompts)
		case <-ticker.C:
		}
	}
}

func TestEngineHandleCommandDispatch_BuiltinUnknownPermissionAndI18n(t *testing.T) {
	t.Run("help dispatch", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		if !e.handleCommand(p, &Message{SessionKey: "test:help", UserID: "user", ReplyCtx: "ctx"}, "/help") {
			t.Fatal("handleCommand(/help) = false, want true")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "Available Commands") {
			t.Fatalf("/help sent = %#v, want English help text", sent)
		}
	})

	t.Run("unknown command not consumed", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		if e.handleCommand(p, &Message{SessionKey: "test:unknown", UserID: "user", ReplyCtx: "ctx"}, "/does-not-exist arg") {
			t.Fatal("unknown command should not be consumed")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "`/does-not-exist` is not a cc-connect command") {
			t.Fatalf("unknown command sent = %#v", sent)
		}
	})

	t.Run("disabled command denial", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()
		e.SetDisabledCommands([]string{"help"})

		if !e.handleCommand(p, &Message{SessionKey: "test:disabled", UserID: "user", ReplyCtx: "ctx"}, "/help") {
			t.Fatal("disabled builtin should be consumed")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "Command `/help` is disabled") {
			t.Fatalf("disabled command sent = %#v", sent)
		}
	})

	t.Run("privileged command denial", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		if !e.handleCommand(p, &Message{SessionKey: "test:shell", UserID: "user", ReplyCtx: "ctx"}, "/shell pwd") {
			t.Fatal("unauthorized privileged command should be consumed")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "Command `/shell` requires admin privilege") {
			t.Fatalf("privileged command sent = %#v", sent)
		}
	})

	t.Run("invalid args and localized language", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		if !e.handleCommand(p, &Message{SessionKey: "test:lang", UserID: "user", ReplyCtx: "ctx"}, "/lang nope") {
			t.Fatal("invalid /lang should be consumed")
		}
		if !strings.Contains(p.getSent()[0], "Unknown language") {
			t.Fatalf("invalid lang sent = %#v", p.getSent())
		}

		p.clearSent()
		if !e.handleCommand(p, &Message{SessionKey: "test:lang", UserID: "user", ReplyCtx: "ctx"}, "/lang zh") {
			t.Fatal("/lang zh should be consumed")
		}
		if !strings.Contains(p.getSent()[0], "语言已切换") {
			t.Fatalf("localized lang sent = %#v", p.getSent())
		}
	})
}

func TestEngineHandleCommandDispatch_CustomPromptAndExecPermission(t *testing.T) {
	t.Run("custom prompt command dispatches to agent", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		session := newDispatchRecordingSession("custom-session")
		agent := &dispatchRecordingAgent{next: []*dispatchRecordingSession{session}}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()
		e.SetInjectSender(false)
		e.commands.Add("triage", "triage issue", "Triage {{1}} {{2*:now}}", "", "", "config")

		if !e.handleCommand(p, &Message{SessionKey: "test:custom", UserID: "user", ReplyCtx: "ctx"}, "/triage INC-1 please now") {
			t.Fatal("custom command should be consumed")
		}
		prompts := waitForDispatchPrompts(t, session, 1)
		if prompts[0] != "Triage INC-1 please now" {
			t.Fatalf("custom command prompt = %q", prompts[0])
		}
		sent := waitForDispatchSent(t, p, 1)
		if !strings.Contains(sent[0], "done:Triage INC-1 please now") {
			t.Fatalf("custom command result sent = %#v", sent)
		}
	})

	t.Run("custom exec command requires admin", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		defer e.Stop()
		e.commands.Add("ops", "", "", "pwd", "", "config")

		if !e.handleCommand(p, &Message{SessionKey: "test:exec", UserID: "user", ReplyCtx: "ctx"}, "/ops") {
			t.Fatal("custom exec command should be consumed")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "Command `/ops` requires admin privilege") {
			t.Fatalf("custom exec denial sent = %#v", sent)
		}
	})

	t.Run("disabled custom command is denied before execution", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		session := newDispatchRecordingSession("disabled-custom-session")
		agent := &dispatchRecordingAgent{next: []*dispatchRecordingSession{session}}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()
		e.commands.Add("triage", "triage issue", "Triage {{args}}", "", "", "config")
		e.SetDisabledCommands([]string{"triage"})

		if !e.handleCommand(p, &Message{SessionKey: "test:disabled-custom", UserID: "user", ReplyCtx: "ctx"}, "/triage INC-2") {
			t.Fatal("disabled custom command should be consumed")
		}
		sent := p.getSent()
		if len(sent) != 1 || !strings.Contains(sent[0], "Command `/triage` is disabled") {
			t.Fatalf("disabled custom command sent = %#v", sent)
		}
		if prompts := session.sentPrompts(); len(prompts) != 0 {
			t.Fatalf("disabled custom command should not reach agent, prompts = %#v", prompts)
		}
	})
}

func TestEngineHandleCommandDispatch_CommandManagement(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("demo", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.Stop()
	msg := &Message{SessionKey: "test:commands", UserID: "admin", ReplyCtx: "ctx"}
	e.SetAdminFrom("admin")

	if !e.handleCommand(p, msg, "/commands add") {
		t.Fatal("invalid /commands add should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "Usage: `/commands add") {
		t.Fatalf("invalid /commands add sent = %#v", sent)
	}

	p.clearSent()
	if !e.handleCommand(p, msg, "/commands add triage Review {{args}}") {
		t.Fatal("/commands add should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "Command `/triage` added") {
		t.Fatalf("/commands add sent = %#v", sent)
	}

	p.clearSent()
	if !e.handleCommand(p, msg, "/commands") {
		t.Fatal("/commands list should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "/triage") {
		t.Fatalf("/commands list sent = %#v", sent)
	}

	p.clearSent()
	if !e.handleCommand(p, msg, "/commands addexec --work-dir /tmp ops pwd") {
		t.Fatal("/commands addexec should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "Exec command `/ops` added") {
		t.Fatalf("/commands addexec sent = %#v", sent)
	}
	if cmd, ok := e.commands.Resolve("ops"); !ok || cmd.Exec != "pwd" || cmd.WorkDir != "/tmp" {
		t.Fatalf("ops command = %+v, ok=%v", cmd, ok)
	}

	p.clearSent()
	if !e.handleCommand(p, msg, "/commands del missing") {
		t.Fatal("/commands del missing should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "Command `/missing` not found") {
		t.Fatalf("/commands del missing sent = %#v", sent)
	}

	p.clearSent()
	if !e.handleCommand(p, msg, "/commands del triage") {
		t.Fatal("/commands del should be consumed")
	}
	if sent := p.getSent(); len(sent) != 1 || !strings.Contains(sent[0], "Command `/triage` removed") {
		t.Fatalf("/commands del sent = %#v", sent)
	}
	if _, ok := e.commands.Resolve("triage"); ok {
		t.Fatal("triage should not resolve after deletion")
	}
}

func TestEngineReceiveMessage_SessionOrchestrationNewResumeValidationAndMissingAgent(t *testing.T) {
	t.Run("new session starts fresh and saves reported agent id", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		session := newDispatchRecordingSession("fresh-agent-id")
		agent := &dispatchRecordingAgent{next: []*dispatchRecordingSession{session}}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		msg := &Message{SessionKey: "test:new:u1", Platform: "test", UserID: "u1", UserName: "user", Content: "hello", ReplyCtx: "ctx"}
		e.ReceiveMessage(p, msg)

		waitForDispatchPrompts(t, session, 1)
		if got := agent.starts(); len(got) != 1 || got[0] != "" {
			t.Fatalf("StartSession args = %#v, want one fresh start", got)
		}
		active := e.sessions.GetOrCreateActive(msg.SessionKey)
		if got := active.GetAgentSessionID(); got != "fresh-agent-id" {
			t.Fatalf("stored agent session id = %q, want fresh-agent-id", got)
		}
	})

	t.Run("resume passes existing valid agent id", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		session := newDispatchRecordingSession("resumed-agent-id")
		agent := &dispatchRecordingAgent{next: []*dispatchRecordingSession{session}}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		msg := &Message{SessionKey: "test:resume:u1", Platform: "test", UserID: "u1", UserName: "user", Content: "continue", ReplyCtx: "ctx"}
		active := e.sessions.GetOrCreateActive(msg.SessionKey)
		active.SetAgentSessionID("saved-agent-id", agent.Name())
		e.ReceiveMessage(p, msg)

		waitForDispatchPrompts(t, session, 1)
		if got := agent.starts(); len(got) != 1 || got[0] != "saved-agent-id" {
			t.Fatalf("StartSession args = %#v, want saved-agent-id resume", got)
		}
		if got := active.GetAgentSessionID(); got != "resumed-agent-id" {
			t.Fatalf("stored forked session id = %q, want resumed-agent-id", got)
		}
	})

	t.Run("invalid stored id is cleared before start", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		session := newDispatchRecordingSession("replacement-agent-id")
		agent := &dispatchRecordingAgent{
			next: []*dispatchRecordingSession{session},
			validate: func(context.Context, string) bool {
				return false
			},
		}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		msg := &Message{SessionKey: "test:invalid:u1", Platform: "test", UserID: "u1", UserName: "user", Content: "start clean", ReplyCtx: "ctx"}
		active := e.sessions.GetOrCreateActive(msg.SessionKey)
		active.SetAgentSessionID("stale-agent-id", agent.Name())
		e.ReceiveMessage(p, msg)

		waitForDispatchPrompts(t, session, 1)
		if got := agent.starts(); len(got) != 1 || got[0] != "" {
			t.Fatalf("StartSession args = %#v, want invalid id cleared", got)
		}
		if got := active.GetAgentSessionID(); got != "replacement-agent-id" {
			t.Fatalf("stored replacement session id = %q, want replacement-agent-id", got)
		}
		if _, ok := e.sessions.KnownAgentSessionIDs()["stale-agent-id"]; !ok {
			t.Fatal("stale id should be retained in past IDs for owned-session filtering")
		}
	})

	t.Run("missing agent start failure leaves session unlocked", func(t *testing.T) {
		p := &stubPlatformEngine{n: "test"}
		agent := &dispatchRecordingAgent{startErr: errors.New("agent binary missing")}
		e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
		defer e.Stop()

		msg := &Message{SessionKey: "test:missing:u1", Platform: "test", UserID: "u1", UserName: "user", Content: "hello", ReplyCtx: "ctx"}
		e.ReceiveMessage(p, msg)

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if len(agent.starts()) == 1 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := agent.starts(); len(got) != 1 || got[0] != "" {
			t.Fatalf("StartSession args = %#v, want one failed fresh start", got)
		}
		active := e.sessions.GetOrCreateActive(msg.SessionKey)
		if active.Busy() {
			t.Fatal("session should be unlocked after missing agent failure")
		}
	})
}

func TestEngineReceiveMessage_WorkspaceBindingUsesWorkspaceSession(t *testing.T) {
	baseDir := t.TempDir()
	wsDir := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := normalizeWorkspacePath(wsDir)
	agentName := "p11-workspace-agent"
	var created []*dispatchRecordingAgent
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		a := &dispatchRecordingAgent{
			name:    agentName,
			workDir: opts["work_dir"].(string),
			next:    []*dispatchRecordingSession{newDispatchRecordingSession("workspace-agent-id")},
		}
		created = append(created, a)
		return a, nil
	})

	parent := &dispatchRecordingAgent{name: agentName, next: []*dispatchRecordingSession{newDispatchRecordingSession("global-agent-id")}}
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", parent, []Platform{p}, "", LangEnglish)
	defer e.Stop()
	e.SetMultiWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	channelID := "C-workspace"
	e.workspaceBindings.Bind("project:test", workspaceChannelKey("test", channelID), "workspace chat", workspace)

	msg := &Message{
		SessionKey: "test:" + channelID + ":u1",
		Platform:   "test",
		ChannelID:  channelID,
		UserID:     "u1",
		UserName:   "user",
		Content:    "run in workspace",
		ReplyCtx:   "ctx",
	}
	e.ReceiveMessage(p, msg)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(created) == 1 && len(created[0].starts()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(created) != 1 {
		t.Fatalf("workspace agents created = %d, want 1", len(created))
	}
	if created[0].workDir != workspace {
		t.Fatalf("workspace agent workDir = %q, want %q", created[0].workDir, workspace)
	}
	if got := created[0].starts(); len(got) != 1 || got[0] != "" {
		t.Fatalf("workspace StartSession args = %#v, want fresh start", got)
	}
	if got := parent.starts(); len(got) != 0 {
		t.Fatalf("global parent StartSession args = %#v, want none", got)
	}
	wsState := e.workspacePool.Get(workspace)
	if wsState == nil || wsState.sessions == nil {
		t.Fatal("workspace state/session manager was not initialized")
	}
	if got := wsState.sessions.GetOrCreateActive(msg.SessionKey).GetAgentSessionID(); got != "workspace-agent-id" {
		t.Fatalf("workspace stored agent id = %q, want workspace-agent-id", got)
	}
	if got := e.sessions.GetOrCreateActive(msg.SessionKey).GetAgentSessionID(); got != "" {
		t.Fatalf("global session id = %q, want empty because workspace handled the message", got)
	}
}
