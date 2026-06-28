package core

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type scheduledRunTarget struct {
	platformName      string
	sessionKey        string
	runSessionKey     string
	platform          Platform
	effectivePlatform Platform
	replyCtx          any
}

func (e *Engine) resolveScheduledRunTarget(sessionKey, title, kind string, mute bool) (scheduledRunTarget, error) {
	platformName := ""
	if idx := strings.Index(sessionKey, ":"); idx > 0 {
		platformName = sessionKey[:idx]
	}

	var targetPlatform Platform
	for _, p := range e.platforms {
		if p.Name() == platformName {
			targetPlatform = p
			break
		}
	}
	// In multi-workspace mode the stored session key may be prefixed with the
	// workspace path. Search for a known platform marker and strip the prefix
	// only for this execution; persisted scheduler state stays unchanged.
	if targetPlatform == nil {
		for _, p := range e.platforms {
			needle := ":" + p.Name() + ":"
			if idx := strings.Index(sessionKey, needle); idx >= 0 {
				targetPlatform = p
				platformName = p.Name()
				sessionKey = sessionKey[idx+1:]
				break
			}
		}
	}
	if targetPlatform == nil {
		return scheduledRunTarget{}, fmt.Errorf("platform %q not found for session %q", platformName, sessionKey)
	}

	rc, ok := targetPlatform.(ReplyContextReconstructor)
	if !ok {
		return scheduledRunTarget{}, fmt.Errorf("platform %q does not support proactive messaging (%s)", platformName, kind)
	}

	runSessionKey := sessionKey
	var replyCtx any
	var err error
	if !mute {
		if resolver, ok := targetPlatform.(CronReplyTargetResolver); ok {
			resolvedSessionKey, resolvedReplyCtx, err := resolver.ResolveCronReplyTarget(sessionKey, title)
			if err != nil {
				if !errors.Is(err, ErrNotSupported) {
					return scheduledRunTarget{}, fmt.Errorf("resolve %s reply target: %w", kind, err)
				}
			} else {
				if resolvedSessionKey != "" {
					runSessionKey = resolvedSessionKey
				}
				if resolvedReplyCtx != nil {
					replyCtx = resolvedReplyCtx
				}
			}
		}
	}
	if replyCtx == nil {
		replyCtx, err = rc.ReconstructReplyCtx(runSessionKey)
		if err != nil {
			return scheduledRunTarget{}, fmt.Errorf("reconstruct reply context: %w", err)
		}
	}

	effectivePlatform := targetPlatform
	if mute {
		effectivePlatform = &mutePlatform{targetPlatform}
	}

	return scheduledRunTarget{
		platformName:      platformName,
		sessionKey:        sessionKey,
		runSessionKey:     runSessionKey,
		platform:          targetPlatform,
		effectivePlatform: effectivePlatform,
		replyCtx:          replyCtx,
	}, nil
}

func (e *Engine) sendScheduledStartNotice(p Platform, replyCtx any, muted, silent bool, description string) {
	if muted || silent {
		return
	}
	e.send(p, replyCtx, fmt.Sprintf("⏰ %s", description))
}

func (e *Engine) expandScheduledPrompt(prompt string) string {
	if !strings.HasPrefix(prompt, "/") {
		return prompt
	}
	parts := strings.Fields(prompt)
	if len(parts) == 0 {
		return prompt
	}
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	if skill := e.skills.Resolve(cmd); skill != nil {
		return BuildSkillInvocationPrompt(skill, parts[1:])
	}
	return prompt
}

func (e *Engine) resolveScheduledWorkContext(kind string, targetPlatform Platform, sessionKey, workDir string) (Agent, *SessionManager, string) {
	agent := e.agent
	sessions := e.sessions
	workspaceDir := ""

	if e.multiWorkspace {
		channelID := extractChannelID(sessionKey)
		if channelID != "" {
			workspace, _, err := e.resolveWorkspace(targetPlatform, channelID)
			if err == nil && workspace != "" {
				wsAgent, wsSessions, _, effectiveDir, err := e.workspaceContext(workspace, sessionKey)
				if err == nil {
					agent = wsAgent
					sessions = wsSessions
					workspaceDir = effectiveDir
				}
			}
		}
	}

	if workDir != "" {
		wsAgent, wsSessions, err := e.getOrCreateWorkspaceAgent(workDir)
		if err == nil {
			agent = wsAgent
			sessions = wsSessions
			workspaceDir = workDir
		} else {
			slog.Warn(kind+": workspace agent creation failed, using global",
				"work_dir", workDir, "session_key", sessionKey, "error", err)
		}
	}

	return agent, sessions, workspaceDir
}

type scheduledAgentRun struct {
	kind              string
	jobID             string
	platform          Platform
	msg               *Message
	agent             Agent
	sessions          *SessionManager
	workspaceDir      string
	baseSessionKey    string
	runSessionKey     string
	useNewSession     bool
	sideSessionPrefix string
	compositePrefix   string
	mute              bool
	requireResponse   bool
}

func (e *Engine) runScheduledAgentMessage(run scheduledAgentRun) error {
	if run.useNewSession {
		run.msg.SessionKey = run.runSessionKey
		session := run.sessions.NewSideSession(run.runSessionKey, run.sideSessionPrefix+run.jobID)
		if !session.TryLock() {
			return fmt.Errorf("session %q is busy", run.runSessionKey)
		}
		iKey := fmt.Sprintf("%s#%s:%s", run.runSessionKey, run.compositePrefix, session.ID)
		if run.workspaceDir != "" {
			iKey = run.workspaceDir + ":" + iKey
		}
		prevHistLen := session.HistoryLen()
		e.processInteractiveMessageWith(run.platform, run.msg, session, run.agent, run.sessions, iKey, run.workspaceDir, run.runSessionKey)
		e.cleanupInteractiveState(iKey)
		if run.requireResponse && !run.mute && session.HistoryLen() < prevHistLen+2 {
			return fmt.Errorf("%s job %q produced an empty response", run.kind, run.jobID)
		}
		return nil
	}

	session := run.sessions.GetOrCreateActive(run.baseSessionKey)
	if !session.TryLock() {
		return fmt.Errorf("session %q is busy", run.baseSessionKey)
	}

	iKey := run.baseSessionKey
	if run.workspaceDir != "" {
		iKey = run.workspaceDir + ":" + run.baseSessionKey
	}
	prevHistLen := session.HistoryLen()
	e.processInteractiveMessageWith(run.platform, run.msg, session, run.agent, run.sessions, iKey, run.workspaceDir, run.baseSessionKey)
	if run.requireResponse && !run.mute && session.HistoryLen() < prevHistLen+2 {
		return fmt.Errorf("%s job %q produced an empty response", run.kind, run.jobID)
	}
	return nil
}
