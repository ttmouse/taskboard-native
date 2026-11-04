#!/usr/bin/env python3
"""Small stdlib-only writer and indexer for the project optimization backlog."""
from __future__ import annotations

import argparse
import datetime as dt
import html
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ITEMS = ROOT / "backlog" / "items"
VALID_STATUS = {"inbox", "accepted", "doing", "done", "blocked", "rejected", "deferred"}
VALID_IMPACT = {"high", "medium", "low"}
VALID_EFFORT = {"S", "M", "L"}
VALID_CATEGORY = {"accessibility", "performance", "reliability", "security", "product", "developer-experience"}


def slugify(value: str) -> str:
    value = re.sub(r"[^a-zA-Z0-9\u4e00-\u9fff]+", "-", value.lower()).strip("-")
    return value[:48].rstrip("-") or "optimization"


def read_items() -> list[dict[str, str]]:
    rows = []
    for path in sorted(ITEMS.glob("*/*.md")):
        content = path.read_text(encoding="utf-8")
        match = re.match(r"\A---\n(.*?)\n---\n(.*)\Z", content, re.S)
        if not match:
            raise ValueError(f"invalid front matter: {path}")
        meta: dict[str, str] = {}
        for line in match.group(1).splitlines():
            key, sep, value = line.partition(":")
            if not sep:
                raise ValueError(f"invalid metadata line in {path}: {line}")
            meta[key.strip()] = json.loads(value.strip())
        meta["body"] = match.group(2)
        meta["path"] = str(path.relative_to(ROOT))
        rows.append(meta)
    return rows


def write_item(meta: dict[str, str], body: str) -> Path:
    path = ITEMS / dt.date.today().strftime("%Y-%m") / f"{meta['id']}-{meta['slug']}.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    fields = ["id", "title", "slug", "status", "impact", "effort", "category", "created", "evidence", "evidence_status", "evidence_note", "verification", "resolution"]
    front = "\n".join(f"{key}: {json.dumps(meta.get(key, ''), ensure_ascii=False)}" for key in fields)
    path.write_text(f"---\n{front}\n---\n\n{body.rstrip()}\n", encoding="utf-8")
    return path


def cmd_add(args: argparse.Namespace) -> None:
    if args.impact not in VALID_IMPACT or args.effort not in VALID_EFFORT:
        raise ValueError("impact must be high|medium|low and effort must be S|M|L")
    items = read_items()
    number = max((int(re.search(r"OP-(\d+)", x["id"]).group(1)) for x in items), default=0) + 1
    meta = {
        "id": f"OP-{number:03d}", "title": args.title, "slug": slugify(args.title),
        "status": "inbox", "impact": args.impact, "effort": args.effort,
        "category": args.category, "created": dt.date.today().isoformat(),
        "evidence": args.evidence, "evidence_status": args.evidence_status,
        "evidence_note": args.evidence_note, "resolution": "",
    }
    body = "## 现状与证据\n\n" + args.context + "\n\n## 建议方向\n\n" + args.direction + "\n\n## 验收标准\n\n" + args.acceptance + "\n\n## 决策记录\n\n- 初始状态：inbox（待筛选，未经批准实施）。"
    path = write_item(meta, body)
    print(f"{meta['id']} {path.relative_to(ROOT)}")


def cmd_set(args: argparse.Namespace) -> None:
    if args.status not in VALID_STATUS:
        raise ValueError(f"status must be one of {', '.join(sorted(VALID_STATUS))}")
    matches = [x for x in read_items() if x["id"].upper() == args.id.upper()]
    if len(matches) != 1:
        raise ValueError(f"expected one item for {args.id}, found {len(matches)}")
    item = matches[0]
    if args.status == "done" and not (args.verification or item.get("verification")):
        raise ValueError("done requires --verification with an acceptance record path")
    meta = {k: item.get(k, "") for k in ("id", "title", "slug", "status", "impact", "effort", "category", "created", "evidence", "evidence_status", "evidence_note", "verification", "resolution")}
    meta["status"] = args.status
    if args.evidence:
        meta["evidence"] = args.evidence
    if args.verification:
        meta["verification"] = args.verification
    if args.message:
        meta["resolution"] = (meta.get("resolution", "") + "\n- " + dt.date.today().isoformat() + ": " + args.message).strip()
    body = item["body"]
    if args.message:
        body = body.rstrip() + "\n\n" + args.message + "\n"
    old_path = ROOT / item["path"]
    new_path = write_item(meta, body)
    if old_path != new_path:
        old_path.unlink()
        if old_path.parent.exists() and not any(old_path.parent.iterdir()):
            old_path.parent.rmdir()
    print(f"{meta['id']} -> {meta['status']}")


def cmd_dup(args: argparse.Namespace) -> None:
    q = args.query.casefold().strip()
    hits = [x for x in read_items() if q in (x["title"] + " " + x["body"]).casefold()]
    if not hits:
        print("no matches")
        return
    for x in hits:
        print(f"{x['id']} [{x['status']}] {x['title']} — {x['path']}")


def cmd_stats(_: argparse.Namespace) -> None:
    items = read_items()
    for status in sorted(VALID_STATUS):
        count = sum(x["status"] == status for x in items)
        if count:
            print(f"{status}: {count}")
    print(f"total: {len(items)}")


def cmd_index(_: argparse.Namespace) -> None:
    items = read_items()
    rank = {"high": 0, "medium": 1, "low": 2}
    erank = {"S": 0, "M": 1, "L": 2}
    items.sort(key=lambda x: (x["status"] != "inbox", rank.get(x["impact"], 9), erank.get(x["effort"], 9), x["id"]))
    lines = ["# 优化项索引", "", f"生成时间：{dt.datetime.now().astimezone().isoformat(timespec='minutes')}", "", "| ID | 状态 | 影响 | 工作量 | 标题 |", "|---|---|---|---|---|"]
    for x in items:
        lines.append(f"| {x['id']} | {x['status']} | {x['impact']} | {x['effort']} | [{x['title']}]({x['path']}) |")
    lines.append("")
    (ROOT / "backlog" / "INDEX.md").write_text("\n".join(lines), encoding="utf-8")
    cards = []
    for x in items:
        detail_path = x["path"].removeprefix("backlog/")
        cards.append(f"<article><small>{html.escape(x['id'])} · {html.escape(x['status'])} · {html.escape(x['impact'])} impact · {html.escape(x['effort'])} effort</small><h2>{html.escape(x['title'])}</h2><p>{html.escape(x['body'].split('## 建议方向', 1)[0].replace('## 现状与证据', '').strip()[:300])}</p><a href=\"{html.escape(detail_path)}\">查看条目</a></article>")
    doc = "<!doctype html><meta charset='utf-8'><title>优化项索引</title><style>body{font:16px system-ui;max-width:1000px;margin:40px auto;padding:0 20px;background:#f5f6f8;color:#202124}article{background:white;border:1px solid #ddd;border-radius:12px;padding:18px;margin:14px 0}small{color:#596273}h2{font-size:20px;margin:8px 0}a{color:#245bd6}</style><h1>taskboard-native 优化项</h1>" + "\n".join(cards)
    (ROOT / "backlog" / "INDEX.html").write_text(doc + "\n", encoding="utf-8")
    print(f"indexed {len(items)} item(s)")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    add = sub.add_parser("add", help="add an inbox item")
    add.add_argument("--title", required=True)
    add.add_argument("--impact", required=True, choices=sorted(VALID_IMPACT))
    add.add_argument("--effort", required=True, choices=sorted(VALID_EFFORT))
    add.add_argument("--category", required=True, choices=sorted(VALID_CATEGORY))
    add.add_argument("--evidence", required=True)
    add.add_argument("--evidence-status", default="verified")
    add.add_argument("--evidence-note", default="")
    add.add_argument("--context", required=True)
    add.add_argument("--direction", required=True)
    add.add_argument("--acceptance", required=True)
    add.set_defaults(func=cmd_add)
    setp = sub.add_parser("set", help="change item state")
    setp.add_argument("id")
    setp.add_argument("status", choices=sorted(VALID_STATUS))
    setp.add_argument("--evidence")
    setp.add_argument("--verification", help="acceptance evidence path required to mark done")
    setp.add_argument("-m", "--message")
    setp.set_defaults(func=cmd_set)
    dup = sub.add_parser("dup", help="search all items for duplicates")
    dup.add_argument("query")
    dup.set_defaults(func=cmd_dup)
    stats = sub.add_parser("stats", help="count items by state")
    stats.set_defaults(func=cmd_stats)
    index = sub.add_parser("index", help="rebuild Markdown and HTML indexes")
    index.set_defaults(func=cmd_index)
    args = parser.parse_args()
    try:
        args.func(args)
    except (OSError, ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(2)


if __name__ == "__main__":
    main()
