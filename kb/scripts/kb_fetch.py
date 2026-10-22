#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""kb_fetch.py — 按需从外部来源取内容到 inbox/（只取需要的，不整包）

为什么要它：raw.githubusercontent.com 在部分网络下不通，但 api.github.com 通。
本脚本统一走 GitHub Contents API（base64），失败再退回 raw。

用法
----
  # 1) 列目录（用于生成/更新索引，不下载内容）
  python scripts/kb_fetch.py list swisskyrepo/PayloadsAllTheThings "SQL Injection"

  # 2) 取单个文件到 inbox/（>300KB 默认拒绝，--force 覆盖）
  python scripts/kb_fetch.py get swisskyrepo/PayloadsAllTheThings "SQL Injection/README.md"

  # 3) 按 sources.json 里的 planned 项批量探活（不动内容）
  python scripts/kb_fetch.py probe

注意
----
- 只落 inbox/，归档由人或 AI 按 prompts/00-补充知识库.md 流程处理。
- 不写凭据、不抓需要登录的页面。
"""
from __future__ import annotations

import base64
import json
import os
import sys
import urllib.parse
import urllib.request
from pathlib import Path

KB = Path(__file__).resolve().parent.parent
INBOX = KB / "inbox"
MAX = 300 * 1024


def _req(url: str) -> dict | str:
    h = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "artex-kb/1.0",
    }
    tok = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if tok:
        h["Authorization"] = "Bearer " + tok  # 未认证仅 60 次/小时，批量补充必须带 token
    r = urllib.request.Request(url, headers=h)
    with urllib.request.urlopen(r, timeout=45) as f:
        raw = f.read()
    try:
        return json.loads(raw)
    except Exception:
        return raw.decode("utf-8", "replace")


def list_dir(repo: str, path: str = "") -> int:
    url = f"https://api.github.com/repos/{repo}/contents/{urllib.parse.quote(path)}"
    d = _req(url)
    if isinstance(d, str):
        print(d[:500]); return 1
    for x in d:
        print(f"{x['type']:5} {x['size']:>9} {x['name']}")
    return 0


def get_file(repo: str, path: str, force: bool = False) -> int:
    INBOX.mkdir(parents=True, exist_ok=True)
    url = f"https://api.github.com/repos/{repo}/contents/{urllib.parse.quote(path)}"
    d = _req(url)
    if isinstance(d, str) or "content" not in d:
        print(f"[fail] {path}: {str(d)[:200]}"); return 1
    size = d.get("size", 0)
    if size > MAX and not force:
        print(f"[skip] {path} {size}B > {MAX}B（需要就加 --force，或只取目录列表）")
        return 2
    data = base64.b64decode(d["content"])
    out = INBOX / Path(path).name
    out.write_bytes(data)
    print(f"[ok] {size}B → {out}")
    return 0


def probe() -> int:
    src = KB / "sources" / "sources.json"
    d = json.loads(src.read_text(encoding="utf-8"))
    bad = 0
    for s in d["sources"]:
        if s.get("status") == "planned":
            print(f"[planned] {s['id']:<12} {s['url']}")
            continue
        url = s["url"].replace("https://github.com/", "https://api.github.com/repos/")
        try:
            _req(url)
            print(f"[live]    {s['id']:<12} {s['url']}")
        except Exception as e:
            bad += 1
            print(f"[fail]    {s['id']:<12} {e}")
    return 1 if bad else 0


def main() -> int:
    a = sys.argv[1:]
    if not a:
        print(__doc__); return 2
    if a[0] == "list":
        return list_dir(a[1], a[2] if len(a) > 2 else "")
    if a[0] == "get":
        return get_file(a[1], a[2], "--force" in a)
    if a[0] == "probe":
        return probe()
    print(__doc__); return 2


if __name__ == "__main__":
    sys.exit(main())
