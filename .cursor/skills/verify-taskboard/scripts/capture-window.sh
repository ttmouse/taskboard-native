#!/usr/bin/env bash
# Capture the front Taskboard (or taskboard) window via Accessibility bounds + screencapture.
# Usage: capture-window.sh /path/to/out.png
# Env: PROCESS=Taskboard (default) | taskboard   OUT must end in .png
set -euo pipefail

OUT="${1:-}"
if [[ -z "$OUT" ]]; then
  echo "Usage: capture-window.sh /path/to/out.png" >&2
  exit 2
fi
PROCESS="${PROCESS:-Taskboard}"
mkdir -p "$(dirname "$OUT")"

# System Events returns "x, y, w, h" style lists; normalize to screencapture -R x,y,w,h
BOUNDS="$(osascript -e "tell application \"System Events\" to tell (first process whose name is \"$PROCESS\") to get {position, size} of window 1" 2>/dev/null || true)"
if [[ -z "$BOUNDS" ]]; then
  echo "FAIL: no window for process '$PROCESS' (is the App/binary open? try PROCESS=taskboard)" >&2
  exit 1
fi

# BOUNDS like "123, 45, 1280, 800" after stripping braces from nested lists
NORM="$(printf '%s' "$BOUNDS" | tr -d '{}' | tr -d ' ' )"
# osascript may return "x, y, w, h" already; ensure four CSV fields
X="$(printf '%s' "$NORM" | cut -d, -f1)"
Y="$(printf '%s' "$NORM" | cut -d, -f2)"
W="$(printf '%s' "$NORM" | cut -d, -f3)"
H="$(printf '%s' "$NORM" | cut -d, -f4)"
if [[ -z "$X" || -z "$Y" || -z "$W" || -z "$H" ]]; then
  echo "FAIL: could not parse bounds: $BOUNDS" >&2
  exit 1
fi

RECT="${X},${Y},${W},${H}"
echo "process=$PROCESS bounds=$RECT -> $OUT"
screencapture -x -R"$RECT" "$OUT"
if [[ ! -s "$OUT" ]]; then
  echo "FAIL: screencapture produced empty file $OUT" >&2
  exit 1
fi
echo "captured=$OUT bytes=$(wc -c < "$OUT" | tr -d ' ')"
