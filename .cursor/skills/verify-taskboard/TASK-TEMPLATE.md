# Verify task template (chapter-10)

Copy this skeleton into the task's working notes / `$EVIDENCE_DIR/plan.md`. Replace every `«…»` placeholder before driving. Do not leave placeholders in Launch / Doctor / Drive commands.

## Identity

| Field | Value |
|---|---|
| FEATURE | «sidebar-toggle \| board-sse \| search \| new-task \| other» |
| VERSION | «must match mygo.json after bump, e.g. 0.1.91» |
| RUN_ID | «YYYYMMDD-HHMMSS or short token» |
| EVIDENCE_DIR | `/tmp/tb-verify-«RUN_ID»/` |
| LAUNCH | «App `/Applications/Taskboard.app` \| isolated binary on 127.0.0.1:47827» |

## OPERATION

«Exact user/agent actions. Example (sidebar stutter): activate Taskboard; send ⌘B 5–8 times rapidly via osascript.»

## EXPECTED

«Observable end state. Example: rail animates crisply; no multi-hundred-ms hitch; prefs async; installed version equals VERSION; before/after show expanded vs collapsed.»

## Chapter-10 steps

1. **doctor** — `cd /Users/douba/Projects/taskboard-native && .cursor/skills/verify-taskboard/scripts/doctor.sh` (exit 0). Unit gate: `go test ./cmd/taskboard/ -count=1` (narrow `-run` OK when feature-scoped).
2. **capture BEFORE** — `.cursor/skills/verify-taskboard/scripts/capture-window.sh "$EVIDENCE_DIR/before.png"`
3. **perform OPERATION** — «paste OPERATION here; no placeholders»
4. **capture AFTER** — `.cursor/skills/verify-taskboard/scripts/capture-window.sh "$EVIDENCE_DIR/after.png"` (+ optional recording)
5. **assert EXPECTED** — «checklist; FAIL closed if any miss»
6. **report** — write ≤20 lines to `$EVIDENCE_DIR/status.txt` with PASS/FAIL, VERSION, paths.

## Install reminder (visible UI acceptance)

```bash
cd /Users/douba/Projects/taskboard-native
python3 scripts/generate_version.py
mygo build -skip-dmg
rm -rf /Applications/Taskboard.app
ditto build/darwin-arm64/Taskboard.app /Applications/Taskboard.app
defaults read /Applications/Taskboard.app/Contents/Info CFBundleShortVersionString
open -a Taskboard
```

## Cleanup

Kill only PIDs in `$EVIDENCE_DIR/pids.txt`. Keep `$EVIDENCE_DIR` proofs. Quit only the App instance this run opened.
