package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type schedulerTargetPlatform struct {
	stubPlatformEngine
	reconstructErr error
	resolveErr     error

	reconstructKeys []string
	resolveCalls    int
	resolveKeys     []string
	resolveTitles   []string
	resolveKey      string
	resolveCtx      any
}

func (p *schedulerTargetPlatform) ReconstructReplyCtx(sessionKey string) (any, error) {
	p.reconstructKeys = append(p.reconstructKeys, sessionKey)
	if p.reconstructErr != nil {
		return nil, p.reconstructErr
	}
	return "reconstructed:" + sessionKey, nil
}

func (p *schedulerTargetPlatform) ResolveCronReplyTarget(sessionKey string, title string) (string, any, error) {
	p.resolveCalls++
	p.resolveKeys = append(p.resolveKeys, sessionKey)
	p.resolveTitles = append(p.resolveTitles, title)
	if p.resolveErr != nil {
		return "", nil, p.resolveErr
	}
	return p.resolveKey, p.resolveCtx, nil
}

type namedSchedulerAgent struct {
	stubAgent
	name string
}

func (a *namedSchedulerAgent) Name() string { return a.name }

func TestResolveScheduledRunTarget_StripsWorkspacePrefixAndResolverOverride(t *testing.T) {
	p := &schedulerTargetPlatform{
		stubPlatformEngine: stubPlatformEngine{n: "feishu"},
		resolveKey:         "feishu:thread-fresh",
		resolveCtx:         "thread-rctx",
	}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	target, err := e.resolveScheduledRunTarget("/tmp/workspace:feishu:C123:U456", "Daily summary", "cron", false)
	if err != nil {
		t.Fatalf("resolveScheduledRunTarget() error = %v", err)
	}

	if target.platformName != "feishu" {
		t.Fatalf("platformName = %q, want feishu", target.platformName)
	}
	if target.sessionKey != "feishu:C123:U456" {
		t.Fatalf("sessionKey = %q, want stripped key", target.sessionKey)
	}
	if target.runSessionKey != "feishu:thread-fresh" {
		t.Fatalf("runSessionKey = %q, want resolver override", target.runSessionKey)
	}
	if target.replyCtx != "thread-rctx" {
		t.Fatalf("replyCtx = %#v, want resolver context", target.replyCtx)
	}
	if target.effectivePlatform != p {
		t.Fatalf("effectivePlatform = %#v, want original platform", target.effectivePlatform)
	}
	if p.resolveCalls != 1 || p.resolveKeys[0] != "feishu:C123:U456" || p.resolveTitles[0] != "Daily summary" {
		t.Fatalf("resolver calls = %d keys=%v titles=%v", p.resolveCalls, p.resolveKeys, p.resolveTitles)
	}
	if len(p.reconstructKeys) != 0 {
		t.Fatalf("ReconstructReplyCtx called despite resolver context: %v", p.reconstructKeys)
	}
}

func TestResolveScheduledRunTarget_MuteSkipsResolverAndWrapsPlatform(t *testing.T) {
	p := &schedulerTargetPlatform{
		stubPlatformEngine: stubPlatformEngine{n: "feishu"},
		resolveErr:         errors.New("resolver should not be called"),
	}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	target, err := e.resolveScheduledRunTarget("feishu:C123:U456", "Muted timer", "timer", true)
	if err != nil {
		t.Fatalf("resolveScheduledRunTarget() error = %v", err)
	}

	if p.resolveCalls != 0 {
		t.Fatalf("ResolveCronReplyTarget called %d time(s) for muted job", p.resolveCalls)
	}
	if len(p.reconstructKeys) != 1 || p.reconstructKeys[0] != "feishu:C123:U456" {
		t.Fatalf("reconstruct keys = %v, want base session key", p.reconstructKeys)
	}
	if _, ok := target.effectivePlatform.(*mutePlatform); !ok {
		t.Fatalf("effectivePlatform = %T, want *mutePlatform", target.effectivePlatform)
	}
	if err := target.effectivePlatform.Reply(context.Background(), target.replyCtx, "discarded"); err != nil {
		t.Fatalf("mutePlatform.Reply() error = %v", err)
	}
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("mutePlatform should discard messages, got %v", sent)
	}
}

func TestResolveScheduledRunTarget_ErrorsForMissingCapabilities(t *testing.T) {
	e := NewEngine("test", &stubAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	defer e.cancel()

	_, err := e.resolveScheduledRunTarget("feishu:C123:U456", "Daily", "cron", false)
	if err == nil || !strings.Contains(err.Error(), "does not support proactive messaging") {
		t.Fatalf("resolveScheduledRunTarget() error = %v, want proactive messaging capability error", err)
	}
}

func TestSendScheduledStartNotice_RespectsMuteAndSilent(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	e.sendScheduledStartNotice(p, "ctx", false, false, "Daily")
	e.sendScheduledStartNotice(p, "ctx", true, false, "Muted")
	e.sendScheduledStartNotice(p, "ctx", false, true, "Silent")

	sent := p.getSent()
	if len(sent) != 1 || sent[0] != "⏰ Daily" {
		t.Fatalf("sent = %v, want only unmuted non-silent notice", sent)
	}
}

func TestResolveScheduledWorkContext_WorkDirUsesWorkspaceAgent(t *testing.T) {
	agentName := "scheduler-workdir-agent"
	var gotWorkDir string
	RegisterAgent(agentName, func(opts map[string]any) (Agent, error) {
		if v, _ := opts["work_dir"].(string); v != "" {
			gotWorkDir = v
		}
		return &namedSchedulerAgent{name: agentName}, nil
	})

	workDir := t.TempDir()
	baseAgent := &namedSchedulerAgent{name: agentName}
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", baseAgent, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	agent, sessions, workspaceDir := e.resolveScheduledWorkContext("cron", p, "feishu:C123:U456", workDir)
	if agent == baseAgent {
		t.Fatal("resolveScheduledWorkContext returned base agent, want workspace agent")
	}
	if sessions == e.sessions {
		t.Fatal("resolveScheduledWorkContext returned base session manager, want workspace session manager")
	}
	if workspaceDir != workDir {
		t.Fatalf("workspaceDir = %q, want %q", workspaceDir, workDir)
	}
	if gotWorkDir != workDir {
		t.Fatalf("factory work_dir = %q, want %q", gotWorkDir, workDir)
	}
}

func TestRunScheduledAgentMessage_NewSessionCreatesSideSession(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	agentSession := newResultAgentSession("scheduled complete")
	agent := &resultAgent{session: agentSession}
	e := NewEngine("test", agent, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	sessions := NewSessionManager("")
	msg := &Message{
		SessionKey: "feishu:C123:U456",
		Platform:   "feishu",
		UserID:     "cron",
		UserName:   "cron",
		Content:    "scheduled prompt",
		ReplyCtx:   "ctx",
	}

	err := e.runScheduledAgentMessage(scheduledAgentRun{
		kind:              "cron",
		jobID:             "job-1",
		platform:          p,
		msg:               msg,
		agent:             agent,
		sessions:          sessions,
		baseSessionKey:    "feishu:C123:U456",
		runSessionKey:     "feishu:C123:U456",
		useNewSession:     true,
		sideSessionPrefix: "cron-",
		compositePrefix:   "cron",
		requireResponse:   true,
	})
	if err != nil {
		t.Fatalf("runScheduledAgentMessage() error = %v", err)
	}

	if msg.SessionKey != "feishu:C123:U456" {
		t.Fatalf("msg.SessionKey = %q, want run session key", msg.SessionKey)
	}
	sideSessions := sessions.ListSessions("feishu:C123:U456")
	if len(sideSessions) != 1 {
		t.Fatalf("side session count = %d, want 1", len(sideSessions))
	}
	if sideSessions[0].Name != "cron-job-1" {
		t.Fatalf("side session name = %q, want cron-job-1", sideSessions[0].Name)
	}
	if sideSessions[0].HistoryLen() < 2 {
		t.Fatalf("side session history len = %d, want user and assistant entries", sideSessions[0].HistoryLen())
	}
	if sent := p.getSent(); len(sent) != 1 || sent[0] != "scheduled complete" {
		t.Fatalf("sent = %v, want scheduled result", sent)
	}
}

func TestRunScheduledAgentMessage_ReturnsBusyForLockedReuseSession(t *testing.T) {
	p := &stubPlatformEngine{n: "feishu"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	sessions := NewSessionManager("")
	session := sessions.GetOrCreateActive("feishu:C123:U456")
	if !session.TryLock() {
		t.Fatal("failed to lock test session")
	}
	defer session.Unlock()

	err := e.runScheduledAgentMessage(scheduledAgentRun{
		kind:           "timer",
		jobID:          "job-1",
		platform:       p,
		msg:            &Message{SessionKey: "feishu:C123:U456", Content: "prompt", ReplyCtx: "ctx"},
		agent:          &stubAgent{},
		sessions:       sessions,
		baseSessionKey: "feishu:C123:U456",
		runSessionKey:  "feishu:C123:U456",
	})
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("runScheduledAgentMessage() error = %v, want busy", err)
	}
}

func TestCronScheduler_RunJob_ProjectMissingMarksLastError(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewCronScheduler(store)
	job := &CronJob{
		ID:         "missing-project",
		Project:    "ghost",
		SessionKey: "feishu:C123:U456",
		CronExpr:   "0 6 * * *",
		Prompt:     "hello",
		Enabled:    true,
		CreatedAt:  time.Now(),
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	scheduler.runJob(job, false)

	found, lastRunSet, lastErr := cronJobRunStatus(store, job.ID)
	if !found {
		t.Fatal("expected stored job")
	}
	if !lastRunSet {
		t.Fatal("LastRun was not set")
	}
	if !strings.Contains(lastErr, `project "ghost" not found`) {
		t.Fatalf("LastError = %q, want missing project", lastErr)
	}
}

func TestCronScheduler_ExecuteJob_LoadsStoredJob(t *testing.T) {
	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scheduler := NewCronScheduler(store)
	platform := &stubCronReplyTargetPlatform{
		stubPlatformEngine: stubPlatformEngine{n: "feishu"},
	}
	agentSession := newResultAgentSession("cron executed")
	engine := NewEngine("test", &resultAgent{session: agentSession}, []Platform{platform}, "", LangEnglish)
	defer engine.cancel()
	engine.cronScheduler = scheduler
	scheduler.RegisterEngine("test", engine)

	job := &CronJob{
		ID:          "execute-stored",
		Project:     "test",
		SessionKey:  "feishu:C123:U456",
		CronExpr:    "0 6 * * *",
		Prompt:      "stored prompt",
		Description: "Stored run",
		Enabled:     true,
		CreatedAt:   time.Now(),
	}
	if err := store.Add(job); err != nil {
		t.Fatal(err)
	}

	scheduler.executeJob(job.ID)
	scheduler.executeJob("missing-job")

	if len(agentSession.sentPrompts) != 1 || !strings.Contains(agentSession.sentPrompts[0], "stored prompt") {
		t.Fatalf("agent prompts = %#v, want stored prompt", agentSession.sentPrompts)
	}
	found, lastRunSet, lastErr := cronJobRunStatus(store, job.ID)
	if !found || !lastRunSet || lastErr != "" {
		t.Fatalf("run status found=%v lastRunSet=%v lastErr=%q, want successful run", found, lastRunSet, lastErr)
	}
}
