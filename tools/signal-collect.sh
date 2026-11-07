#!/usr/bin/env bash
# 只读信号采集：把本项目的可观察状态追加成一行 JSON，供每周分诊使用。
# 不修改 backlog，不启动/关闭 App，不写仓库文件。
set -uo pipefail

ROOT="${PROJECT_ROOT:-/Users/douba/Projects/taskboard-native}"
APP="${TASKBOARD_APP:-/Applications/Taskboard.app}"
OUT_DIR="${TASKBOARD_SIGNAL_DIR:-$HOME/.local/state/taskboard-signals}"
OUT="$OUT_DIR/signals.ndjson"
mkdir -p "$OUT_DIR"

TS="$(date +%Y-%m-%dT%H:%M:%S%z)"

VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$ROOT/mygo.json" 2>/dev/null || echo unknown)"
INSTALLED="$(defaults read "$APP/Contents/Info" CFBundleShortVersionString 2>/dev/null || echo missing)"

BACKLOG="$(cd "$ROOT" && python3 tools/backlog.py stats 2>/dev/null || true)"
BACKLOG_INBOX="$(printf '%s\n' "$BACKLOG" | awk -F': ' '/^inbox:/{print $2}')"
BACKLOG_DOING="$(printf '%s\n' "$BACKLOG" | awk -F': ' '/^doing:/{print $2}')"
BACKLOG_DONE="$(printf '%s\n' "$BACKLOG" | awk -F': ' '/^done:/{print $2}')"
BACKLOG_TOTAL="$(printf '%s\n' "$BACKLOG" | awk -F': ' '/^total:/{print $2}')"

MAP_OUT="$("$HOME/.agents/skills/verify-taskboard/scripts/check-feature-map.sh" 2>&1)"
MAP_RC=$?
MAP_LINE="$(printf '%s\n' "$MAP_OUT" | tail -1)"

DOCTOR_OUT="$(cd "$ROOT" && "$HOME/.agents/skills/verify-taskboard/scripts/doctor.sh" 2>&1)"
DOCTOR_RC=$?
DOCTOR_STATE="$(printf '%s\n' "$DOCTOR_OUT" | awk -F= '/^process_state=/{print $2}')"
DOCTOR_VERSION="$(printf '%s\n' "$DOCTOR_OUT" | awk -F= '/^expected_version=/{print $2}')"

DIRTY="$(cd "$ROOT" && git status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
HEAD="$(cd "$ROOT" && git log -1 --format=%h 2>/dev/null || echo unknown)"

python3 - "$OUT" "$TS" "$VERSION" "$INSTALLED" "$BACKLOG_INBOX" "$BACKLOG_DOING" "$BACKLOG_DONE" "$BACKLOG_TOTAL" "$MAP_RC" "$MAP_LINE" "$DOCTOR_RC" "$DOCTOR_STATE" "$DOCTOR_VERSION" "$DIRTY" "$HEAD" <<'PY'
import json
import sys

out, ts, version, installed, inbox, doing, done, total, map_rc, map_line, doctor_rc, doctor_state, doctor_version, dirty, head = sys.argv[1:]

record = {
    "ts": ts,
    "version": version,
    "installed_version": installed,
    "version_drift": str(version) != str(installed),
    "backlog": {
        "inbox": int(inbox or 0),
        "doing": int(doing or 0),
        "done": int(done or 0),
        "total": int(total or 0),
    },
    "feature_map": {"rc": int(map_rc), "line": map_line},
    "doctor": {"rc": int(doctor_rc), "state": doctor_state, "expected_version": doctor_version},
    "git": {"dirty_files": int(dirty or 0), "head": head},
}
with open(out, "a", encoding="utf-8") as f:
    f.write(json.dumps(record, ensure_ascii=False) + "\n")
print(json.dumps(record, ensure_ascii=False))
PY
