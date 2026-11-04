#!/usr/bin/env bash
# Read-only check: is Taskboard worth driving for verification?
# Compares mygo.json version to installed /Applications/Taskboard.app.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
# Prefer repo root if we are under .cursor/skills/verify-taskboard/scripts
if [[ ! -f "$ROOT/mygo.json" ]]; then
  ROOT="/Users/douba/Projects/taskboard-native"
fi

APP="${TASKBOARD_APP:-/Applications/Taskboard.app}"
REQUIRE_RUNNING=0
ADDR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --require-running) REQUIRE_RUNNING=1; shift ;;
    --addr) ADDR="${2:-}"; shift 2 ;;
    --app) APP="${2:-}"; shift 2 ;;
    -h|--help)
      cat <<EOF
Usage: doctor.sh [--require-running] [--addr HOST:PORT] [--app /path/Taskboard.app]
Checks mygo.json version vs installed App CFBundleShortVersionString.
EOF
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ ! -f "$ROOT/mygo.json" ]]; then
  echo "FAIL: mygo.json missing at $ROOT" >&2
  exit 1
fi

EXPECTED="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$ROOT/mygo.json")"
echo "expected_version=$EXPECTED (from $ROOT/mygo.json)"

if [[ ! -d "$APP" ]]; then
  echo "FAIL: App missing: $APP" >&2
  exit 1
fi

INSTALLED="$(defaults read "$APP/Contents/Info" CFBundleShortVersionString 2>/dev/null || true)"
if [[ -z "$INSTALLED" ]]; then
  echo "FAIL: could not read CFBundleShortVersionString from $APP" >&2
  exit 1
fi
echo "installed_version=$INSTALLED (from $APP)"

if [[ "$INSTALLED" != "$EXPECTED" ]]; then
  echo "FAIL: version mismatch (install/reopen App after bump+build)" >&2
  exit 1
fi

# Optional: process must be running (does not start one).
if [[ "$REQUIRE_RUNNING" -eq 1 ]]; then
  if ! pgrep -x Taskboard >/dev/null 2>&1 && ! pgrep -x taskboard >/dev/null 2>&1; then
    echo "FAIL: neither Taskboard nor taskboard process is running" >&2
    exit 1
  fi
  echo "process=running"
fi

# Optional: HTTP reachability for isolated/review instances.
if [[ -n "$ADDR" ]]; then
  # Prefer a cheap GET; contracts use /api/ on the service.
  if ! curl -fsS --max-time 2 "http://${ADDR}/api/projects" >/dev/null; then
    echo "FAIL: cannot reach http://${ADDR}/api/projects" >&2
    exit 1
  fi
  echo "addr_ok=$ADDR"
fi

echo "doctor=OK version=$INSTALLED"
exit 0
