#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'background-process-e2e: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

usage() {
  cat <<'EOF'
Run a real Feishu end-to-end test that asks a test bot to start a detached
background process, waits for the cc-connect turn to complete, and verifies
that the background process is still alive and appending logs afterwards.

Default target:
  CC_E2E_PROJECT=idiom-chain-bot-b
  CC_E2E_EXPECTED_AGENT=claudecode
  CC_E2E_EXPECT_BACKGROUND_SURVIVES=1

Known current status:
  - Claude Code test bot is expected to pass.
  - Traex uses the same test case but is currently expected to fail because the
    background process exits after the turn completes. To reproduce that known
    failure without failing this script:

    CC_REAL_FEISHU_E2E=1 \
    CC_E2E_PROJECT=idiom-chain-bot \
    CC_E2E_EXPECTED_AGENT=traex \
    CC_E2E_EXPECT_BACKGROUND_SURVIVES=0 \
    tools/e2e/background_process_e2e.sh

Required:
  CC_REAL_FEISHU_E2E=1

Optional:
  CC_E2E_BINARY              default: .tmp/cc-connect
  CC_E2E_CONFIG              default: ~/.cc-connect-test/config.toml
  CC_E2E_CHAT_ID             default: oc_5f719ed782195ecbc449d4c095eea075
  CC_E2E_PROJECT             default: idiom-chain-bot-b
  CC_E2E_EXPECTED_AGENT      default: claudecode
  CC_E2E_TARGET_OPEN_ID      skip open_id extraction from startup logs
  CC_E2E_RUN_ROOT            default: .tmp/background-process-e2e
  CC_E2E_TIMEOUT_SECS        default: 240
  CC_E2E_SETTLE_SECS         default: 5
  CC_E2E_SURVIVAL_WAIT_SECS  default: 8
  CC_E2E_EXPECT_BACKGROUND_SURVIVES  default: 1
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

if [ "${CC_REAL_FEISHU_E2E:-}" != "1" ]; then
  die "refusing to send real Feishu messages unless CC_REAL_FEISHU_E2E=1"
fi

require_cmd python3
require_cmd lark-cli

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
binary="${CC_E2E_BINARY:-$repo_root/.tmp/cc-connect}"
config_path="${CC_E2E_CONFIG:-$HOME/.cc-connect-test/config.toml}"
default_chat_id="oc_5f719ed782195ecbc449d4c095eea075" # 智能体测试开发群
chat_id="${CC_E2E_CHAT_ID:-$default_chat_id}"
project="${CC_E2E_PROJECT:-idiom-chain-bot-b}"
expected_agent="${CC_E2E_EXPECTED_AGENT:-claudecode}"
expect_survival="${CC_E2E_EXPECT_BACKGROUND_SURVIVES:-1}"
run_root="${CC_E2E_RUN_ROOT:-$repo_root/.tmp/background-process-e2e}"
timeout_secs="${CC_E2E_TIMEOUT_SECS:-240}"
settle_secs="${CC_E2E_SETTLE_SECS:-5}"
survival_wait_secs="${CC_E2E_SURVIVAL_WAIT_SECS:-8}"

[ "$expect_survival" = "0" ] || [ "$expect_survival" = "1" ] || die "CC_E2E_EXPECT_BACKGROUND_SURVIVES must be 0 or 1"
[ -f "$config_path" ] || die "test config not found: $config_path"

if [ ! -x "$binary" ]; then
  require_cmd go
  mkdir -p "$(dirname "$binary")"
  printf 'background-process-e2e: building test binary %s\n' "$binary"
  GOCACHE="${GOCACHE:-/private/tmp/cc-connect-go-cache}" go build -o "$binary" ./cmd/cc-connect
fi

config_real="$(python3 - "$config_path" <<'PY'
import os, sys
print(os.path.realpath(os.path.expanduser(sys.argv[1])))
PY
)"
prod_config="$(python3 - <<'PY'
import os
print(os.path.realpath(os.path.expanduser("~/.cc-connect/config.toml")))
PY
)"
[ "$config_real" != "$prod_config" ] || die "refusing to use production config: $config_real"

project_info="$(python3 - "$config_real" "$project" "$expected_agent" <<'PY'
import sys, tomllib

config_path, project_name, expected_agent = sys.argv[1:4]
with open(config_path, "rb") as f:
    cfg = tomllib.load(f)

data_dir = cfg.get("data_dir", "")
if not data_dir or ".cc-connect-test" not in data_dir:
    raise SystemExit(f"config data_dir must be an isolated test dir, got {data_dir!r}")

matches = [p for p in cfg.get("projects") or [] if p.get("name") == project_name]
if len(matches) != 1:
    raise SystemExit(f"expected exactly one project named {project_name!r}, got {len(matches)}")

project = matches[0]
agent_type = (project.get("agent") or {}).get("type", "")
if agent_type != expected_agent:
    raise SystemExit(f"project {project_name!r} agent.type={agent_type!r}, expected {expected_agent!r}")

platforms = [p for p in project.get("platforms", []) if p.get("type") == "feishu"]
if len(platforms) != 1:
    raise SystemExit(f"project {project_name!r} must define exactly one feishu platform, got {len(platforms)}")

opts = platforms[0].get("options", {})
if opts.get("app_secret") and opts.get("app_secret") == "your-secret":
    raise SystemExit("test app_secret placeholder is not usable")

print("project ok")
print(f"name={project_name}")
print(f"agent={agent_type}")
print(f"work_dir={project.get('work_dir', '')}")
print(f"card={opts.get('enable_feishu_card', False)}")
PY
)"
printf '%s\n' "$project_info"

if env | grep -E '^CC_(LOG_FILE|LOG_MAX_SIZE|DATA_DIR|PROJECT|SESSION_KEY)=' >/dev/null; then
  printf 'background-process-e2e: clearing inherited CC_* runtime variables for test process\n' >&2
fi

if pgrep -af "cc-connect.*--config[ =]$config_real" >/dev/null 2>&1; then
  die "another cc-connect test process appears to use this config: $config_real"
fi

mkdir -p "$run_root"
stamp="$(date +%Y%m%d-%H%M%S)"
stdout_log="$run_root/stdout-$stamp.log"
app_log="$run_root/app-$stamp.log"
probe_dir="$run_root/probe-$stamp"
probe_log="$probe_dir/background.log"
pid_file="$probe_dir/background.pid"
bg_pid=""

cleanup() {
  if [ -n "${bg_pid:-}" ] && kill -0 "$bg_pid" >/dev/null 2>&1; then
    kill "$bg_pid" >/dev/null 2>&1 || true
    wait "$bg_pid" >/dev/null 2>&1 || true
  fi
  if [ -n "${pid:-}" ] && kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

(
  unset CC_LOG_FILE CC_LOG_MAX_SIZE CC_DATA_DIR CC_PROJECT CC_SESSION_KEY
  CC_LOG_FILE="$app_log" CC_LOG_MAX_SIZE=10485760 \
    "$binary" --config "$config_real"
) >"$stdout_log" 2>&1 &
pid=$!

wait_log() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  local deadline=$((SECONDS + timeout_secs))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if grep -E "$pattern" "$file" >/dev/null 2>&1; then
      printf 'background-process-e2e: %s ok\n' "$label"
      return 0
    fi
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      printf '%s\n' '--- stdout ---' >&2
      tail -80 "$stdout_log" >&2 || true
      printf '%s\n' '--- app log ---' >&2
      tail -120 "$app_log" >&2 || true
      die "cc-connect test process exited before $label"
    fi
    sleep 1
  done
  printf '%s\n' '--- stdout ---' >&2
  tail -80 "$stdout_log" >&2 || true
  printf '%s\n' '--- app log ---' >&2
  tail -120 "$app_log" >&2 || true
  die "timeout waiting for $label"
}

wait_file() {
  local file="$1"
  local label="$2"
  local deadline=$((SECONDS + timeout_secs))
  while [ "$SECONDS" -lt "$deadline" ]; do
    [ -s "$file" ] && {
      printf 'background-process-e2e: %s ok\n' "$label"
      return 0
    }
    sleep 1
  done
  die "timeout waiting for $label: $file"
}

line_count() {
  if [ -f "$probe_log" ]; then
    wc -l <"$probe_log" | tr -d ' '
  else
    printf '0\n'
  fi
}

wait_log "$stdout_log" 'connected to wss://msg-frontier\.feishu\.cn/ws/v2' "Feishu websocket"
wait_log "$app_log" 'feishu: bot identified.*open_id=' "bot identity"
wait_log "$app_log" "platform ready.*project=$project.*platform=feishu|platform ready.*platform=feishu.*project=$project" "target platform ready"
wait_log "$app_log" 'engine started' "engine started"
wait_log "$app_log" 'cc-connect is running' "process running"

target_open_id="${CC_E2E_TARGET_OPEN_ID:-}"
if [ -z "$target_open_id" ]; then
  target_open_id="$(python3 - "$app_log" "$project" <<'PY'
import re, sys

path, project = sys.argv[1:3]
last_open_id = ""
with open(path, encoding="utf-8", errors="replace") as f:
    for line in f:
        m = re.search(r"bot identified.*open_id=([A-Za-z0-9_]+)", line)
        if m:
            last_open_id = m.group(1)
        if "platform ready" in line and f"project={project}" in line and "platform=feishu" in line:
            if last_open_id:
                print(last_open_id)
                raise SystemExit(0)
raise SystemExit(1)
PY
)" || die "could not extract target bot open_id for project=$project; set CC_E2E_TARGET_OPEN_ID explicitly"
fi

message_text="$(python3 - "$target_open_id" "$stamp" "$probe_dir" "$probe_log" "$pid_file" <<'PY'
import shlex, sys

target_open_id, stamp, probe_dir, probe_log, pid_file = sys.argv[1:6]
cmd = "\n".join([
    f"mkdir -p {shlex.quote(probe_dir)}",
    f"rm -f {shlex.quote(probe_log)} {shlex.quote(pid_file)}",
    "nohup sh -c 'log=\"$1\"; i=0; while :; do i=$((i+1)); printf \"%s %s\\n\" \"$(date +%s)\" \"$i\" >> \"$log\"; sleep 1; done' sh "
    + f"{shlex.quote(probe_log)} </dev/null >/dev/null 2>&1 &",
    f"echo $! > {shlex.quote(pid_file)}",
    f"echo \"BACKGROUND_STARTED pid=$(cat {shlex.quote(pid_file)}) log={probe_log}\"",
])
print(f"""<at user_id="{target_open_id}"></at> background-process-e2e-{stamp}

请执行下面这个后台进程存活测试。不要改动命令，不要等待后台进程结束；命令完成并输出 BACKGROUND_STARTED 后，本轮回复即可结束。

```bash
{cmd}
```
""")
PY
)"

send_json="$(lark-cli im +messages-send --chat-id "$chat_id" --text "$message_text" --as bot --format json)"
message_id="$(SEND_JSON="$send_json" python3 - <<'PY'
import json, os

data = json.loads(os.environ["SEND_JSON"])
if not data.get("ok"):
    raise SystemExit(json.dumps(data, ensure_ascii=False))
print(data["data"]["message_id"])
PY
)"

printf 'background-process-e2e: sent message_id=%s project=%s target_open_id=%s\n' "$message_id" "$project" "$target_open_id"
wait_log "$app_log" "message received.*msg_id=$message_id" "message received"
wait_log "$app_log" "processing message.*session=" "message processing"
wait_log "$app_log" "turn complete.*msg_id=$message_id" "turn complete"

sleep "$settle_secs"
wait_file "$pid_file" "background pid file"
bg_pid="$(tr -cd '0-9' <"$pid_file")"
[ -n "$bg_pid" ] || die "background pid file does not contain a pid: $pid_file"
wait_file "$probe_log" "background log"

lines_after_turn="$(line_count)"
sleep "$survival_wait_secs"
lines_later="$(line_count)"

alive=0
if kill -0 "$bg_pid" >/dev/null 2>&1; then
  alive=1
fi

growing=0
if [ "$lines_later" -gt "$lines_after_turn" ]; then
  growing=1
fi

if [ "$expect_survival" = "1" ]; then
  [ "$alive" = "1" ] || die "background process exited after turn complete: pid=$bg_pid"
  [ "$growing" = "1" ] || die "background log stopped growing after turn complete: $probe_log lines=$lines_after_turn->$lines_later"
  printf 'background-process-e2e: PASS\n'
else
  if [ "$alive" = "1" ] && [ "$growing" = "1" ]; then
    die "known-failing case unexpectedly passed: pid=$bg_pid lines=$lines_after_turn->$lines_later"
  fi
  printf 'background-process-e2e: KNOWN_FAIL_CONFIRMED\n'
fi

printf '  project=%s\n' "$project"
printf '  expected_agent=%s\n' "$expected_agent"
printf '  expect_survival=%s\n' "$expect_survival"
printf '  stdout_log=%s\n' "$stdout_log"
printf '  app_log=%s\n' "$app_log"
printf '  probe_log=%s\n' "$probe_log"
printf '  pid_file=%s\n' "$pid_file"
printf '  background_pid=%s\n' "$bg_pid"
printf '  lines=%s->%s\n' "$lines_after_turn" "$lines_later"
printf '  message_id=%s\n' "$message_id"
