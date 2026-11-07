#!/usr/bin/env python3
"""backlog.py 的写入口回归测试。只在临时目录里操作，不碰真实队列。"""
import argparse
import datetime as dt
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import backlog  # noqa: E402

ITEM = """---
id: "OP-001"
title: "t"
slug: "t"
status: "inbox"
impact: "high"
effort: "M"
category: "performance"
family: ""
created: "2026-11-04"
evidence: "x"
evidence_status: "verified"
evidence_note: ""
verification: ""
resolution: ""
---

## 现状与证据

body
"""


class BacklogSetTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.items = self.root / "backlog" / "items"
        self.month = self.items / dt.date.today().strftime("%Y-%m")
        self.month.mkdir(parents=True)
        self.path = self.month / "OP-001-t.md"
        self.path.write_text(ITEM, encoding="utf-8")
        self.old = (backlog.ROOT, backlog.ITEMS)
        backlog.ROOT = self.root
        backlog.ITEMS = self.items

    def tearDown(self):
        backlog.ROOT, backlog.ITEMS = self.old
        self.tmp.cleanup()

    def args(self, **kw):
        base = {"id": "OP-001", "status": None, "family": None, "evidence": None,
                "verification": None, "message": None}
        base.update(kw)
        return argparse.Namespace(**base)

    def test_family_is_idempotent(self):
        backlog.cmd_set(self.args(family="F1 x"))
        first = self.path.read_text(encoding="utf-8")
        backlog.cmd_set(self.args(family="F1 x"))
        self.assertEqual(first, self.path.read_text(encoding="utf-8"))
        self.assertIn('family: "F1 x"', first)
        self.assertNotIn("\n\n\n## 现状", first)

    def test_merged_requires_message(self):
        with self.assertRaises(ValueError):
            backlog.cmd_set(self.args(status="merged"))

    def test_set_requires_status_or_family(self):
        with self.assertRaises(ValueError):
            backlog.cmd_set(self.args())


if __name__ == "__main__":
    unittest.main()
