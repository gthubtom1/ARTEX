#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""kb_search.py — 知识库检索（BM25-lite，零依赖）

两种用法
--------
1) 命令行：
     python scripts/kb_search.py "ssrf redis 未授权" -n 5
     python scripts/kb_search.py "thinkphp" --type fingerprint --json

2) 作为 ARTEX 自定义工具（kind=script）：
     入参（stdin JSON）：{"query": "...", "limit": 5, "type": "tactic", "tags": ["inject"]}
     出参（stdout JSON）：{"hits":[{"id","path","score","summary"}]}
   注意：只返回**摘要**，正文由 agent 用 Read 工具按 path 读——这是省 token 的关键。

检索对象：kb/index.json（先跑 kb_index.py）
"""
from __future__ import annotations

import json
import math
import re
import sys
from collections import Counter
from pathlib import Path

KB = Path(__file__).resolve().parent.parent
INDEX = KB / "index.json"


def tok(s: str) -> list[str]:
    s = s.lower()
    out = re.findall(r"[a-z0-9_./-]{2,}", s)
    out += re.findall(r"[\u4e00-\u9fff]{2,4}", s)  # 中文按 2-4 字切片，够用
    return out


def load():
    if not INDEX.exists():
        raise SystemExit("index.json 不存在，先运行: python kb/scripts/kb_index.py")
    d = json.loads(INDEX.read_text(encoding="utf-8"))
    return [e for e in d["entries"] if not e.get("duplicate_of") and not e.get("near_duplicate_of")]


def search(query: str, entries: list[dict], limit: int = 5, typ: str | None = None,
           tags: list[str] | None = None) -> list[dict]:
    q = tok(query)
    if not q:
        return []
    pool = entries
    if typ:
        pool = [e for e in pool if e["type"] == typ]
    if tags:
        pool = [e for e in pool if set(tags) & set(e["tags"])]
    if not pool:
        return []
    # idf
    N = len(pool)
    df = Counter()
    for e in pool:
        seen = set(tok(" ".join([e["title"], " ".join(e["tags"]), e["summary"]])))
        for t in seen:
            df[t] += 1
    idf = {t: math.log(1 + (N - df.get(t, 0) + 0.5) / (df.get(t, 0) + 0.5)) for t in q}

    hits = []
    for e in pool:
        title = tok(e["title"] + " " + e["id"])
        tags_t = [t.lower() for t in e["tags"]]
        summ = tok(e["summary"])
        body_path = KB / e["path"]
        body = ""
        if body_path.exists() and body_path.stat().st_size < 400_000:
            body = body_path.read_text(encoding="utf-8", errors="replace")
        bt = Counter(tok(body))
        score = 0.0
        for t in q:
            s = 3.0 * title.count(t) + 3.0 * (1 if t in tags_t else 0) + 2.0 * summ.count(t)
            s += 1.0 * min(bt.get(t, 0), 20) / 20.0
            score += idf[t] * s
        if score > 0:
            hits.append({"id": e["id"], "path": e["path"], "type": e["type"],
                         "tags": e["tags"], "score": round(score, 3), "summary": e["summary"]})
    hits.sort(key=lambda x: -x["score"])
    return hits[:limit]


def main() -> int:
    stdin_json = None
    if not sys.stdin.isatty():
        raw = sys.stdin.read().strip()
        if raw.startswith("{"):
            stdin_json = json.loads(raw)
    if stdin_json is not None:
        hits = search(stdin_json.get("query", ""), load(),
                      int(stdin_json.get("limit") or 5),
                      stdin_json.get("type"), stdin_json.get("tags"))
        print(json.dumps({"hits": hits}, ensure_ascii=False))
        return 0

    args = sys.argv[1:]
    if not args:
        print(__doc__)
        return 2
    q, limit, typ, as_json = args[0], 5, None, "--json" in args
    if "-n" in args:
        limit = int(args[args.index("-n") + 1])
    if "--type" in args:
        typ = args[args.index("--type") + 1]
    hits = search(q, load(), limit, typ)
    if as_json:
        print(json.dumps({"hits": hits}, ensure_ascii=False, indent=1))
        return 0
    if not hits:
        print("（无命中）")
        return 0
    for h in hits:
        print(f"[{h['score']:>6}] {h['id']}  ({h['type']})")
        print(f"         {h['summary'][:100]}")
        print(f"         → {h['path']}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
