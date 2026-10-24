#!/usr/bin/env python3
"""Create three real cards, then overlap their API-driven state changes.

Run while Taskboard is open. All writes use HTTP; the GUI receives SSE.
The three demo cards remain on the board after the run.
"""

import argparse
import concurrent.futures
import json
import time
import urllib.request
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--addr", default="http://127.0.0.1:47826")
    parser.add_argument("--project", default="local")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    events = []

    def request(method, path, body=None):
        req = urllib.request.Request(
            args.addr.rstrip("/") + path,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "X-Agent": "motion-demo"},
            method=method,
        )
        with urllib.request.urlopen(req, timeout=5) as response:
            return json.load(response)

    project = next(p for p in request("GET", "/api/projects") if p["id"] == args.project)
    columns = {c["name"]: c["id"] for c in project["columns"]}
    for name in ("待办", "进行中", "评审", "完成"):
        if name not in columns:
            raise SystemExit(f"项目缺少列：{name}")

    start = time.monotonic()

    def wait_until(offset):
        time.sleep(max(0, start + offset - time.monotonic()))

    def log(action, task):
        event = {"seconds": round(time.monotonic() - start, 3), "action": action,
                 "id": task["id"], "title": task["title"],
                 "columnId": task["columnId"], "version": task["version"]}
        events.append(event)
        print(json.dumps(event, ensure_ascii=False), flush=True)

    cards = []
    for index, letter in enumerate("ABC"):
        wait_until(0.8 + index * 0.18)
        task = request("POST", "/api/tasks", {
            "projectId": args.project, "columnId": columns["待办"],
            "title": f"连续创建 · 卡片 {letter}",
        })
        cards.append(task)
        log("新增", task)

    def move(index, column, offset):
        wait_until(offset)
        current = cards[index]
        updated = request("PATCH", "/api/tasks/" + current["id"], {
            "version": current["version"], "columnId": columns[column], "atTop": True,
        })
        cards[index] = updated
        log("移动到" + column, updated)

    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        # Departures are 120ms apart, shorter than the 420ms spring, so
        # the three cards are in flight together rather than taking turns.
        for offset, column in ((2.0, "进行中"), (3.3, "评审"), (4.6, "完成")):
            futures = [pool.submit(move, i, column, offset + i * 0.12) for i in range(3)]
            for future in futures:
                future.result()
        # Different cards move at the same moment in opposite directions.
        futures = [pool.submit(move, 0, "进行中", 6.1), pool.submit(move, 1, "评审", 6.1)]
        for future in futures:
            future.result()
        futures = [pool.submit(move, i, "完成", 7.6 + i * 0.12) for i in range(2)]
        for future in futures:
            future.result()

    for task in cards:
        actual = request("GET", "/api/tasks/" + task["id"])
        if actual["columnId"] != columns["完成"]:
            raise RuntimeError(f"最终状态未落库：{task['id']}")
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(events, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
