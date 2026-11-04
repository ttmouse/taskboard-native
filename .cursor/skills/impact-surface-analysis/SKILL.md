---
name: impact-surface-analysis
description: Use before dispatching or testing any requirement: enumerate functional essence, all trigger paths, and intermediate states; generate test cases from that list; missing a case blocks submission.
---

# Impact-surface analysis

Run this **before** implementation or verification. Do not skip for "small" UI or shortcut requests.

## Gate
Missing any case derived below **blocks submission**. Screenshots of the whole screen are not evidence when the metric is input-to-first-frame.

## Steps
1. **Essence** — One sentence: the user-visible capability behind the wording (e.g. "sidebar open/closed", not "Cmd+B").
2. **Trigger paths** — List every way to invoke it: keyboard shortcut(s), toolbar/icon click, menu item, gesture, API/RPC, deep link, automation. If unknown, search the code; do not invent.
3. **Intermediate states** — List phases the system can be in mid-action: key-down vs key-up, press vs release, partial animation frames, pending prefs write, focus/foreground requirements.
4. **Cases** — From (2)×(3), write one timed/asserted case per relevant pair. Name the metric (prefer in-process / window-local first-frame). State pass bar.
5. **Anti-cases** — Note what does *not* count (full-screen capture latency, wrong window, background app).
6. **Hand-off** — Put the case list in the verify skill / task prompt. Implementation without this list is incomplete.

## Output shape (keep short)
```
Essence:
Triggers:
- ...
States:
- ...
Cases:
- [id] trigger + state → metric / pass
Blockers if missing:
- ...
```
