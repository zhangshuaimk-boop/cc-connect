//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
	"github.com/stretchr/testify/require"
)

type regressionSentMessage struct {
	content  string
	replyCtx any
}

type regressionPlatform struct {
	mu           sync.Mutex
	name         string
	channelNames map[string]string
	handler      core.MessageHandler
	sent         []regressionSentMessage
	notify       chan struct{}
}

func newRegressionPlatform(name string) *regressionPlatform {
	return &regressionPlatform{
		name:         name,
		channelNames: make(map[string]string),
		notify:       make(chan struct{}, 1),
	}
}

func (p *regressionPlatform) Name() string { return p.name }

func (p *regressionPlatform) Start(handler core.MessageHandler) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = handler
	return nil
}

func (p *regressionPlatform) Reply(_ context.Context, replyCtx any, content string) error {
	return p.Send(context.Background(), replyCtx, content)
}

func (p *regressionPlatform) Send(_ context.Context, replyCtx any, content string) error {
	p.mu.Lock()
	p.sent = append(p.sent, regressionSentMessage{content: content, replyCtx: replyCtx})
	p.mu.Unlock()
	select {
	case p.notify <- struct{}{}:
	default:
	}
	return nil
}

func (p *regressionPlatform) Stop() error { return nil }

func (p *regressionPlatform) ResolveChannelName(channelID string) (string, error) {
	if name, ok := p.channelNames[channelID]; ok {
		return name, nil
	}
	return "", fmt.Errorf("unknown channel %q", channelID)
}

func (p *regressionPlatform) ReconstructReplyCtx(sessionKey string) (any, error) {
	return "reconstructed:" + sessionKey, nil
}

func (p *regressionPlatform) messages() []regressionSentMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]regressionSentMessage, len(p.sent))
	copy(out, p.sent)
	return out
}

func (p *regressionPlatform) waitFor(t *testing.T, match func(regressionSentMessage) bool) regressionSentMessage {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, msg := range p.messages() {
			if match(msg) {
				return msg
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for matching message; got %#v", p.messages())
		}
		select {
		case <-p.notify:
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type queuedRegressionAgent struct {
	session *queuedRegressionSession
}

func (a *queuedRegressionAgent) Name() string { return "queued-regression-agent" }
func (a *queuedRegressionAgent) StartSession(context.Context, string) (core.AgentSession, error) {
	return a.session, nil
}
func (a *queuedRegressionAgent) ListSessions(context.Context) ([]core.AgentSessionInfo, error) {
	return nil, nil
}
func (a *queuedRegressionAgent) Stop() error { return nil }

type queuedRegressionSession struct {
	mu        sync.Mutex
	id        string
	alive     bool
	events    chan core.Event
	prompts   []string
	promptCh  chan string
	sendDone  chan chan struct{}
	closeOnce sync.Once
}

func newQueuedRegressionSession(id string) *queuedRegressionSession {
	return &queuedRegressionSession{
		id:       id,
		alive:    true,
		events:   make(chan core.Event, 16),
		promptCh: make(chan string, 16),
		sendDone: make(chan chan struct{}, 16),
	}
}

func (s *queuedRegressionSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	s.mu.Lock()
	s.prompts = append(s.prompts, prompt)
	s.mu.Unlock()
	s.promptCh <- prompt
	done := make(chan struct{})
	s.sendDone <- done
	<-done
	return nil
}

func (s *queuedRegressionSession) RespondPermission(string, core.PermissionResult) error { return nil }
func (s *queuedRegressionSession) Events() <-chan core.Event                             { return s.events }
func (s *queuedRegressionSession) CurrentSessionID() string                              { return s.id }
func (s *queuedRegressionSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}
func (s *queuedRegressionSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.alive = false
		s.mu.Unlock()
		close(s.events)
	})
	return nil
}

func (s *queuedRegressionSession) waitPrompt(t *testing.T, needle string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case prompt := <-s.promptCh:
			if strings.Contains(prompt, needle) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for prompt containing %q", needle)
		}
	}
}

func (s *queuedRegressionSession) finishSend(t *testing.T) {
	t.Helper()
	select {
	case done := <-s.sendDone:
		close(done)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Send to start")
	}
}

func TestIntegration_RegressionQueuedMessageKeepsReplyContext(t *testing.T) {
	session := newQueuedRegressionSession("queue-regression")
	platform := newRegressionPlatform("mock")
	engine := core.NewEngine("queue-regression", &queuedRegressionAgent{session: session}, []core.Platform{platform}, "", core.LangEnglish)
	defer engine.Stop()

	engine.ReceiveMessage(platform, &core.Message{
		SessionKey: "mock:chat:user",
		Platform:   platform.Name(),
		MessageID:  "msg-first",
		UserID:     "user",
		UserName:   "User",
		Content:    "first turn",
		ReplyCtx:   "ctx-first",
	})
	session.waitPrompt(t, "first turn")

	engine.ReceiveMessage(platform, &core.Message{
		SessionKey: "mock:chat:user",
		Platform:   platform.Name(),
		MessageID:  "msg-second",
		UserID:     "user",
		UserName:   "User",
		Content:    "second turn",
		ReplyCtx:   "ctx-second",
	})
	platform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == "ctx-second" && strings.TrimSpace(msg.content) != ""
	})

	session.events <- core.Event{Type: core.EventText, Content: "first-result"}
	session.events <- core.Event{Type: core.EventResult, Content: "first-result", Done: true}
	session.finishSend(t)
	platform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == "ctx-first" && strings.Contains(msg.content, "first-result")
	})

	session.waitPrompt(t, "second turn")
	session.events <- core.Event{Type: core.EventText, Content: "second-result"}
	session.events <- core.Event{Type: core.EventResult, Content: "second-result", Done: true}
	session.finishSend(t)
	platform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == "ctx-second" && strings.Contains(msg.content, "second-result")
	})
}

type relayRegressionPlatform struct {
	*regressionPlatform
	callerSessionKey string
}

func (p *relayRegressionPlatform) RelayGroupVisibilityKey(callerSessionKey string) (string, bool) {
	p.callerSessionKey = callerSessionKey
	return callerSessionKey + ":thread", true
}

type relayRegressionAgent struct {
	response string
}

func (a *relayRegressionAgent) Name() string { return "relay-regression-agent" }
func (a *relayRegressionAgent) StartSession(context.Context, string) (core.AgentSession, error) {
	return newRelayRegressionSession(a.response), nil
}
func (a *relayRegressionAgent) ListSessions(context.Context) ([]core.AgentSessionInfo, error) {
	return nil, nil
}
func (a *relayRegressionAgent) Stop() error { return nil }

type relayRegressionSession struct {
	response string
	events   chan core.Event
}

func newRelayRegressionSession(response string) *relayRegressionSession {
	return &relayRegressionSession{response: response, events: make(chan core.Event, 1)}
}

func (s *relayRegressionSession) Send(string, []core.ImageAttachment, []core.FileAttachment) error {
	s.events <- core.Event{Type: core.EventResult, Content: s.response, Done: true}
	return nil
}
func (s *relayRegressionSession) RespondPermission(string, core.PermissionResult) error { return nil }
func (s *relayRegressionSession) Events() <-chan core.Event                             { return s.events }
func (s *relayRegressionSession) CurrentSessionID() string                              { return "relay-session" }
func (s *relayRegressionSession) Alive() bool                                           { return true }
func (s *relayRegressionSession) Close() error                                          { return nil }

func TestIntegration_RegressionRelayVisibilityUsesThreadTarget(t *testing.T) {
	sourcePlatform := &relayRegressionPlatform{regressionPlatform: newRegressionPlatform("feishu")}
	targetPlatform := newRegressionPlatform("feishu")
	sourceEngine := core.NewEngine("source", &relayRegressionAgent{response: "unused"}, []core.Platform{sourcePlatform}, "", core.LangEnglish)
	targetEngine := core.NewEngine("target", &relayRegressionAgent{response: "relay response"}, []core.Platform{targetPlatform}, "", core.LangEnglish)
	defer sourceEngine.Stop()
	defer targetEngine.Stop()

	manager := core.NewRelayManager("")
	manager.RegisterEngine("source", sourceEngine)
	manager.RegisterEngine("target", targetEngine)
	manager.Bind("feishu", "chat-1", map[string]string{
		"source": "Source Bot",
		"target": "Target Bot",
	})

	sourceSessionKey := "feishu:chat-1:user-1"
	resp, err := manager.Send(context.Background(), core.RelayRequest{
		From:       "source",
		To:         "target",
		SessionKey: sourceSessionKey,
		Message:    "relay request",
	})
	require.NoError(t, err)
	require.Equal(t, "relay response", resp.Response)
	require.Equal(t, sourceSessionKey, sourcePlatform.callerSessionKey)

	wantReplyCtx := "reconstructed:" + sourceSessionKey + ":thread"
	sourcePlatform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == wantReplyCtx && strings.Contains(msg.content, "relay request")
	})
	targetPlatform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == wantReplyCtx && strings.Contains(msg.content, "relay response")
	})
}

type runAsRegressionAgent struct {
	name      string
	workDir   string
	runAsUser string
	runAsEnv  []string
}

func (a *runAsRegressionAgent) Name() string { return a.name }
func (a *runAsRegressionAgent) StartSession(context.Context, string) (core.AgentSession, error) {
	return newRunAsRegressionSession(a.workDir), nil
}
func (a *runAsRegressionAgent) ListSessions(context.Context) ([]core.AgentSessionInfo, error) {
	return nil, nil
}
func (a *runAsRegressionAgent) Stop() error           { return nil }
func (a *runAsRegressionAgent) GetRunAsUser() string  { return a.runAsUser }
func (a *runAsRegressionAgent) GetRunAsEnv() []string { return append([]string(nil), a.runAsEnv...) }
func (a *runAsRegressionAgent) GetModel() string      { return "" }
func (a *runAsRegressionAgent) GetMode() string       { return "" }

type runAsRegressionSession struct {
	workDir string
	events  chan core.Event
}

func newRunAsRegressionSession(workDir string) *runAsRegressionSession {
	return &runAsRegressionSession{workDir: workDir, events: make(chan core.Event, 1)}
}

func (s *runAsRegressionSession) Send(prompt string, _ []core.ImageAttachment, _ []core.FileAttachment) error {
	s.events <- core.Event{Type: core.EventResult, Content: "workspace=" + s.workDir + " prompt=" + prompt, Done: true}
	return nil
}
func (s *runAsRegressionSession) RespondPermission(string, core.PermissionResult) error { return nil }
func (s *runAsRegressionSession) Events() <-chan core.Event                             { return s.events }
func (s *runAsRegressionSession) CurrentSessionID() string                              { return "runas-session" }
func (s *runAsRegressionSession) Alive() bool                                           { return true }
func (s *runAsRegressionSession) Close() error                                          { return nil }

func TestIntegration_RegressionMultiWorkspacePropagatesRunAsOptions(t *testing.T) {
	baseDir := t.TempDir()
	workspaceDir := filepath.Join(baseDir, "workspace-a")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))
	require.NoError(t, core.AtomicWriteFile(filepath.Join(workspaceDir, ".keep"), []byte("ok"), 0o644))
	canonicalWorkspaceDir, err := filepath.EvalSymlinks(workspaceDir)
	require.NoError(t, err)

	agentName := "integration-runas-regression-agent"
	var (
		mu           sync.Mutex
		capturedOpts []map[string]any
	)
	core.RegisterAgent(agentName, func(opts map[string]any) (core.Agent, error) {
		snapshot := make(map[string]any, len(opts))
		for k, v := range opts {
			snapshot[k] = v
		}
		mu.Lock()
		capturedOpts = append(capturedOpts, snapshot)
		mu.Unlock()

		workDir, _ := opts["work_dir"].(string)
		return &runAsRegressionAgent{name: agentName, workDir: workDir}, nil
	})

	platform := newRegressionPlatform("mock")
	platform.channelNames["channel-a"] = "workspace-a"
	parent := &runAsRegressionAgent{
		name:      agentName,
		runAsUser: "target-user",
		runAsEnv:  []string{"CUSTOM_ENV", "ANOTHER_ENV"},
	}
	engine := core.NewEngine("runas-regression", parent, []core.Platform{platform}, filepath.Join(t.TempDir(), "sessions.json"), core.LangEnglish)
	engine.SetMultiWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	defer engine.Stop()

	engine.ReceiveMessage(platform, &core.Message{
		SessionKey: "mock:channel-a:user",
		Platform:   platform.Name(),
		MessageID:  "msg-runas",
		ChannelID:  "channel-a",
		UserID:     "user",
		UserName:   "User",
		Content:    "use workspace",
		ReplyCtx:   "ctx-runas",
	})

	platform.waitFor(t, func(msg regressionSentMessage) bool {
		return msg.replyCtx == "ctx-runas" && strings.Contains(msg.content, "workspace=")
	})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, capturedOpts, 1)
	require.Equal(t, canonicalWorkspaceDir, capturedOpts[0]["work_dir"])
	require.Equal(t, "target-user", capturedOpts[0]["run_as_user"])
	require.Equal(t, []string{"CUSTOM_ENV", "ANOTHER_ENV"}, capturedOpts[0]["run_as_env"])
}
