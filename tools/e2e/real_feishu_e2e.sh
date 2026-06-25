#!/usr/bin/env bash
set -euo pipefail

die() {
  printf 'real-feishu-e2e: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

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
run_root="${CC_E2E_RUN_ROOT:-$repo_root/.tmp/real-feishu-e2e}"
timeout_secs="${CC_E2E_TIMEOUT_SECS:-180}"
settle_secs="${CC_E2E_SETTLE_SECS:-5}"

[ -x "$binary" ] || die "test binary is not executable: $binary"
[ -f "$config_path" ] || die "test config not found: $config_path"

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

python3 - "$config_real" <<'PY'
import sys, tomllib
with open(sys.argv[1], "rb") as f:
    cfg = tomllib.load(f)
data_dir = cfg.get("data_dir", "")
if not data_dir or ".cc-connect-test" not in data_dir:
    raise SystemExit(f"config data_dir must be an isolated test dir, got {data_dir!r}")
projects = cfg.get("projects") or []
if not projects:
    raise SystemExit("config must define at least one project")
for p in projects:
    for platform in p.get("platforms", []):
        opts = platform.get("options", {})
        if opts.get("app_secret") and opts.get("app_secret") == "your-secret":
            raise SystemExit("test app_secret placeholder is not usable")
print("config ok")
PY

if env | grep -E '^CC_(LOG_FILE|LOG_MAX_SIZE|DATA_DIR|PROJECT|SESSION_KEY)=' >/dev/null; then
  printf 'real-feishu-e2e: clearing inherited CC_* runtime variables for test process\n' >&2
fi

if pgrep -af "cc-connect.*--config[ =]$config_real" >/dev/null 2>&1; then
  die "another cc-connect test process appears to use this config: $config_real"
fi

mkdir -p "$run_root"
stamp="$(date +%Y%m%d-%H%M%S)"
stdout_log="$run_root/stdout-$stamp.log"
app_log="$run_root/app-$stamp.log"

cleanup() {
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
      printf 'real-feishu-e2e: %s ok\n' "$label"
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

wait_log "$stdout_log" 'connected to wss://msg-frontier\.feishu\.cn/ws/v2' "Feishu websocket"
wait_log "$app_log" 'feishu: bot identified.*open_id=' "bot identity"
wait_log "$app_log" 'platform ready.*platform=feishu' "platform ready"
wait_log "$app_log" 'engine started' "engine started"
wait_log "$app_log" 'cc-connect is running' "process running"

target_open_id="$(python3 - "$app_log" <<'PY'
import re, sys
for line in open(sys.argv[1], encoding="utf-8", errors="replace"):
    m = re.search(r"bot identified.*open_id=([A-Za-z0-9_]+)", line)
    if m:
        print(m.group(1))
        break
PY
)"
[ -n "$target_open_id" ] || die "could not extract target bot open_id from app log"

message_text="<at user_id=\"$target_open_id\"></at> real-feishu-e2e-$stamp 开始"
send_json="$(lark-cli im +messages-send --chat-id "$chat_id" --text "$message_text" --as bot --format json)"
message_id="$(SEND_JSON="$send_json" python3 - <<'PY'
import json, sys
import os
data = json.loads(os.environ["SEND_JSON"])
if not data.get("ok"):
    raise SystemExit(json.dumps(data, ensure_ascii=False))
print(data["data"]["message_id"])
PY
)"

printf 'real-feishu-e2e: sent message_id=%s target_open_id=%s\n' "$message_id" "$target_open_id"
wait_log "$app_log" "message received.*msg_id=$message_id" "message received"
wait_log "$app_log" "processing message.*session=" "message processing"
wait_log "$app_log" "turn complete.*msg_id=$message_id" "turn complete"

# The engine logs "turn complete" before all platform-side send cleanup can be
# fully flushed. Keep the process alive briefly so Feishu replies are not
# canceled by this script's cleanup path.
sleep "$settle_secs"

printf 'real-feishu-e2e: PASS\n'
printf '  stdout_log=%s\n' "$stdout_log"
printf '  app_log=%s\n' "$app_log"
printf '  message_id=%s\n' "$message_id"
