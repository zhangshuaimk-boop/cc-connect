package core

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

type builtinCommand = struct {
	names []string
	id    string
}

type parsedEngineCommand struct {
	raw  string
	name string
	args []string
	id   string
}

type engineCommandHandler func(e *Engine, p Platform, msg *Message, args []string, raw string) bool

// builtinCommands maps canonical command names to their aliases/full names.
// The first entry is the canonical name used for prefix matching.
var builtinCommands = []builtinCommand{
	{[]string{"new"}, "new"},
	{[]string{"list", "sessions"}, "list"},
	{[]string{"switch"}, "switch"},
	{[]string{"name", "rename"}, "name"},
	{[]string{"current"}, "current"},
	{[]string{"status"}, "status"},
	{[]string{"usage", "quota"}, "usage"},
	{[]string{"history"}, "history"},
	{[]string{"allow"}, "allow"},
	{[]string{"model"}, "model"},
	{[]string{"reasoning", "effort"}, "reasoning"},
	{[]string{"mode"}, "mode"},
	{[]string{"lang"}, "lang"},
	{[]string{"quiet"}, "quiet"},
	{[]string{"provider"}, "provider"},
	{[]string{"memory"}, "memory"},
	{[]string{"cron"}, "cron"},
	{[]string{"timer", "at", "remind"}, "timer"},
	{[]string{"heartbeat", "hb"}, "heartbeat"},
	{[]string{"compress", "compact"}, "compress"},
	{[]string{"stop"}, "stop"},
	{[]string{"cancel"}, "cancel"},
	{[]string{"help"}, "help"},
	{[]string{"version"}, "version"},
	{[]string{"commands", "command", "cmd"}, "commands"},
	{[]string{"skills", "skill"}, "skills"},
	{[]string{"config"}, "config"},
	{[]string{"doctor"}, "doctor"},
	{[]string{"upgrade", "update"}, "upgrade"},
	{[]string{"restart"}, "restart"},
	{[]string{"alias"}, "alias"},
	{[]string{"delete", "del", "rm"}, "delete"},
	{[]string{"bind"}, "bind"},
	{[]string{"search", "find"}, "search"},
	{[]string{"shell", "sh", "exec", "run"}, "shell"},
	{[]string{"show"}, "show"},
	{[]string{"dir", "cd", "chdir", "workdir"}, "dir"},
	{[]string{"tts"}, "tts"},
	{[]string{"workspace", "ws"}, "workspace"},
	{[]string{"whoami", "myid"}, "whoami"},
	{[]string{"web"}, "web"},
	{[]string{"diff"}, "diff"},
	{[]string{"ps", "btw"}, "ps"},
}

var builtinCommandHandlers = map[string]engineCommandHandler{
	"new": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdNew(p, msg, args)
		return true
	},
	"list": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdList(p, msg, args)
		return true
	},
	"switch": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdSwitch(p, msg, args)
		return true
	},
	"name": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdName(p, msg, args)
		return true
	},
	"current": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdCurrent(p, msg)
		return true
	},
	"status": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdStatus(p, msg)
		return true
	},
	"usage": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdUsage(p, msg)
		return true
	},
	"history": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdHistory(p, msg, args)
		return true
	},
	"allow": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdAllow(p, msg, args)
		return true
	},
	"model": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdModel(p, msg, args)
		return true
	},
	"reasoning": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdReasoning(p, msg, args)
		return true
	},
	"mode": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdMode(p, msg, args)
		return true
	},
	"lang": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdLang(p, msg, args)
		return true
	},
	"quiet": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdQuiet(p, msg, args)
		return true
	},
	"provider": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdProvider(p, msg, args)
		return true
	},
	"memory": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdMemory(p, msg, args)
		return true
	},
	"cron": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdCron(p, msg, args)
		return true
	},
	"timer": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdTimer(p, msg, args)
		return true
	},
	"heartbeat": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdHeartbeat(p, msg, args)
		return true
	},
	"compress": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdCompress(p, msg)
		return true
	},
	"stop": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdStop(p, msg)
		return true
	},
	"cancel": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdCancel(p, msg)
		return true
	},
	"help": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdHelp(p, msg)
		return true
	},
	"start": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdStart(p, msg)
		return true
	},
	"version": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.reply(p, msg.ReplyCtx, VersionInfo)
		return true
	},
	"commands": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdCommands(p, msg, args)
		return true
	},
	"skills": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdSkills(p, msg)
		return true
	},
	"config": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdConfig(p, msg, args)
		return true
	},
	"doctor": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdDoctor(p, msg)
		return true
	},
	"upgrade": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdUpgrade(p, msg, args)
		return true
	},
	"restart": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdRestart(p, msg)
		return true
	},
	"alias": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdAlias(p, msg, args)
		return true
	},
	"delete": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdDelete(p, msg, args)
		return true
	},
	"bind": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdBind(p, msg, args)
		return true
	},
	"search": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdSearch(p, msg, args)
		return true
	},
	"shell": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdShell(p, msg, raw)
		return true
	},
	"diff": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdDiff(p, msg, raw)
		return true
	},
	"show": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdShow(p, msg, args)
		return true
	},
	"dir": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdDir(p, msg, args)
		return true
	},
	"tts": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdTTS(p, msg, args)
		return true
	},
	"workspace": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		if !e.multiWorkspace {
			e.reply(p, msg.ReplyCtx, e.i18n.T(MsgWsNotEnabled))
			return true
		}
		e.handleWorkspaceCommand(p, msg, args)
		return true
	},
	"whoami": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdWhoami(p, msg)
		return true
	},
	"web": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdWeb(p, msg, args)
		return true
	},
	"ps": func(e *Engine, p Platform, msg *Message, args []string, raw string) bool {
		e.cmdPs(p, msg, args)
		return true
	},
}

// privilegedCommands are commands that require admin_from authorization.
var privilegedCommands = map[string]bool{
	"shell":   true,
	"show":    true,
	"dir":     true,
	"restart": true,
	"upgrade": true,
	"web":     true,
	"diff":    true,
}

// AddAlias registers a command alias.
func (e *Engine) AddAlias(name, command string) {
	e.aliasMu.Lock()
	defer e.aliasMu.Unlock()
	e.aliases[name] = command
}

func (e *Engine) SetAliasSaveAddFunc(fn func(name, command string) error) {
	e.aliasSaveAddFunc = fn
}

func (e *Engine) SetAliasSaveDelFunc(fn func(name string) error) {
	e.aliasSaveDelFunc = fn
}

// ClearAliases removes all aliases (for config reload).
func (e *Engine) ClearAliases() {
	e.aliasMu.Lock()
	defer e.aliasMu.Unlock()
	e.aliases = make(map[string]string)
}

// resolveAlias checks if the content (or its first word) matches an alias and replaces it.
func (e *Engine) resolveAlias(content string) string {
	e.aliasMu.RLock()
	defer e.aliasMu.RUnlock()

	if len(e.aliases) == 0 {
		return content
	}

	// Exact match on full content
	if cmd, ok := e.aliases[content]; ok {
		return cmd
	}

	// Match first word, append remaining args
	parts := strings.SplitN(content, " ", 2)
	if cmd, ok := e.aliases[parts[0]]; ok {
		if len(parts) > 1 {
			return cmd + " " + parts[1]
		}
		return cmd
	}
	return content
}

// resolveDisabledCmds resolves a list of command names (including "*" wildcard)
// to a set of canonical command IDs.
func resolveDisabledCmds(cmds []string) map[string]bool {
	m := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		c = strings.ToLower(strings.TrimPrefix(c, "/"))
		if c == "*" {
			for _, bc := range builtinCommands {
				m[bc.id] = true
			}
			return m
		}
		if id := matchPrefix(c, builtinCommands); id != "" {
			m[id] = true
		} else {
			m[c] = true
		}
	}
	return m
}

// GetDisabledCommands returns the list of disabled command IDs for this project.
func (e *Engine) GetDisabledCommands() []string {
	e.userRolesMu.RLock()
	defer e.userRolesMu.RUnlock()
	out := make([]string, 0, len(e.disabledCmds))
	for k := range e.disabledCmds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetDisabledCommands sets the list of command IDs that are disabled for this project.
func (e *Engine) SetDisabledCommands(cmds []string) {
	e.userRolesMu.Lock()
	defer e.userRolesMu.Unlock()
	e.disabledCmds = resolveDisabledCmds(cmds)
}

// SetUserRoles configures per-user role-based policies. Pass nil to disable.
func (e *Engine) SetUserRoles(urm *UserRoleManager) {
	e.userRolesMu.Lock()
	defer e.userRolesMu.Unlock()
	if e.userRoles != nil {
		e.userRoles.Stop()
	}
	e.userRoles = urm
}

// SetAdminFrom sets the admin allowlist for privileged commands.
// "*" means all users who pass allow_from are admins.
// Empty string means privileged commands are denied for everyone.
func (e *Engine) SetAdminFrom(adminFrom string) {
	e.userRolesMu.Lock()
	e.adminFrom = strings.TrimSpace(adminFrom)
	af := e.adminFrom
	shellDisabled := e.disabledCmds["shell"]
	e.userRolesMu.Unlock()
	if af == "" && !shellDisabled {
		slog.Warn("admin_from is not set — privileged commands (/shell, /show, /dir, /restart, /upgrade) are blocked. "+
			"Set admin_from in config to enable them, or use disabled_commands to hide them.",
			"project", e.name)
	}
}

// isAdmin checks whether the given user ID is authorized for privileged commands.
// Unlike AllowList, empty adminFrom means deny-all (fail-closed).
func (e *Engine) isAdmin(userID string) bool {
	e.userRolesMu.RLock()
	af := e.adminFrom
	e.userRolesMu.RUnlock()
	if af == "" {
		return false
	}
	if af == "*" {
		return true
	}
	for _, id := range strings.Split(af, ",") {
		if strings.EqualFold(strings.TrimSpace(id), userID) {
			return true
		}
	}
	return false
}

// matchPrefix finds a unique command matching the given prefix.
// Returns the command id or "" if no match / ambiguous.
func matchPrefix(prefix string, candidates []builtinCommand) string {
	// Exact match first
	for _, c := range candidates {
		for _, n := range c.names {
			if prefix == n {
				return c.id
			}
		}
	}
	// Prefix match
	var matched string
	for _, c := range candidates {
		for _, n := range c.names {
			if strings.HasPrefix(n, prefix) {
				if matched != "" && matched != c.id {
					return "" // ambiguous
				}
				matched = c.id
				break
			}
		}
	}
	return matched
}

// matchSubCommand does prefix matching against a flat list of subcommand names.
func matchSubCommand(input string, candidates []string) string {
	for _, c := range candidates {
		if input == c {
			return c
		}
	}
	var matched string
	for _, c := range candidates {
		if strings.HasPrefix(c, input) {
			if matched != "" {
				return input // ambiguous -> return raw input (will hit default)
			}
			matched = c
		}
	}
	if matched != "" {
		return matched
	}
	return input
}

// splitCommandArgs splits a command string into tokens, respecting single- and
// double-quoted groups so paths like "/workspace bind '/my path/foo'" work
// correctly (#1211). Quotes are stripped from the resulting tokens.
func splitCommandArgs(s string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func parseEngineCommand(raw string) parsedEngineCommand {
	parts := splitCommandArgs(raw)
	if len(parts) == 0 {
		return parsedEngineCommand{raw: raw}
	}
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	return parsedEngineCommand{
		raw:  raw,
		name: name,
		args: parts[1:],
		id:   matchPrefix(name, builtinCommands),
	}
}

func (e *Engine) effectiveDisabledCommands(userID string) map[string]bool {
	e.userRolesMu.RLock()
	disabledCmds := e.disabledCmds
	urm := e.userRoles
	e.userRolesMu.RUnlock()
	if urm != nil {
		if role := urm.ResolveRole(userID); role != nil {
			disabledCmds = role.DisabledCmds
		}
	}
	return disabledCmds
}

func commandDisabled(disabledCmds map[string]bool, commandName string) bool {
	if disabledCmds == nil {
		return false
	}
	return disabledCmds[strings.ToLower(strings.TrimPrefix(commandName, "/"))]
}

func (e *Engine) replyCommandDisabled(p Platform, msg *Message, commandName string) {
	e.reply(p, msg.ReplyCtx, fmt.Sprintf(e.i18n.T(MsgCommandDisabled), "/"+commandName))
}

func (e *Engine) auditCommandBlocked(msg *Message, commandName, reason string) {
	slog.Info("audit: command_blocked",
		"user_id", msg.UserID, "platform", msg.Platform,
		"project", e.name, "command", commandName, "reason", reason)
}

func (e *Engine) auditCommandExecuted(msg *Message, commandName string, attrs ...any) {
	args := []any{
		"user_id", msg.UserID, "platform", msg.Platform,
		"project", e.name, "command", commandName,
	}
	args = append(args, attrs...)
	slog.Info("audit: command_executed", args...)
}

func (e *Engine) handleCommand(p Platform, msg *Message, raw string) bool {
	parsed := parseEngineCommand(raw)
	if parsed.name == "" {
		return false
	}

	disabledCmds := e.effectiveDisabledCommands(msg.UserID)

	if parsed.id != "" && commandDisabled(disabledCmds, parsed.id) {
		e.auditCommandBlocked(msg, parsed.id, "disabled")
		e.replyCommandDisabled(p, msg, parsed.id)
		return true
	}

	if parsed.id != "" && privilegedCommands[parsed.id] && !e.isAdmin(msg.UserID) {
		e.auditCommandBlocked(msg, parsed.id, "unauthorized")
		e.reply(p, msg.ReplyCtx, fmt.Sprintf(e.i18n.T(MsgAdminRequired), "/"+parsed.id))
		return true
	}

	if parsed.id != "" {
		e.auditCommandExecuted(msg, parsed.id)
		if handler, ok := builtinCommandHandlers[parsed.id]; ok {
			return handler(e, p, msg, parsed.args, parsed.raw)
		}
	}

	if custom, ok := e.commands.Resolve(parsed.name); ok {
		if commandDisabled(disabledCmds, custom.Name) {
			e.auditCommandBlocked(msg, custom.Name, "disabled")
			e.replyCommandDisabled(p, msg, custom.Name)
			return true
		}
		e.auditCommandExecuted(msg, custom.Name, "type", "custom")
		e.executeCustomCommand(p, msg, custom, parsed.args)
		return true
	}
	if skill := e.skills.Resolve(parsed.name); skill != nil {
		if commandDisabled(disabledCmds, skill.Name) {
			e.auditCommandBlocked(msg, skill.Name, "disabled")
			e.replyCommandDisabled(p, msg, skill.Name)
			return true
		}
		e.auditCommandExecuted(msg, skill.Name, "type", "skill")
		e.executeSkill(p, msg, skill, parsed.args)
		return true
	}

	// Not a cc-connect command — notify user, then fall through to agent.
	e.send(p, msg.ReplyCtx, fmt.Sprintf(e.i18n.T(MsgUnknownCommand), "/"+parsed.name))
	return false
}
