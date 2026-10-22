#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""kb_index.py — 扫描 kb/ 重建索引（唯一事实源 index.json + 人类可读 INDEX.md）

规则
----
1. 每个条目必须是 Markdown 文件，头部带 frontmatter：
   ---
   id: tactics/sqli          # 必填，全局唯一
   type: tactic              # tactic|payload|surface|fingerprint|case|source|doc
   tags: [inject, sqli]      # 必填，检索主键
   source: self|pat|nuclei|seclists|cwe|web
   license: MIT
   version: 1
   updated: 2026-10-22
   dedupe_key: ""            # case 必填；其它可空
   ---
2. 去重：
   - 精确重复：body 规范化后 sha256 相同 → 后者标记 duplicate_of，不删文件（可回溯）
   - 近似重复：同 type 内 3-gram Jaccard >= 0.90 → 后者标记 near_duplicate_of
   - case 额外按 dedupe_key 精确去重（指纹+手法+面+payload 类）
3. 退出码：0 正常；1 有重复（用于 CI/定时任务告警）；2 结构错误

用法
----
  python scripts/kb_index.py            # 重建
  python scripts/kb_index.py --check    # 校验 index.json 是否过期
  python scripts/kb_index.py --strict   # 有重复即失败
"""
from __future__ import annotations

import hashlib
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

KB = Path(__file__).resolve().parent.parent
DOC_DIRS = ("tactics", "payloads", "surfaces", "fingerprints", "cases", "capabilities", "prompts")
NEAR_DUP = 0.90


def parse_fm(text: str) -> tuple[dict, str]:
    """解析极简 frontmatter。返回 (meta, body)。"""
    if not text.startswith("---"):
        return {}, text
    end = text.find("\n---", 3)
    if end < 0:
        return {}, text
    head, body = text[3:end], text[end + 4:]
    meta: dict = {}
    for line in head.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or ":" not in line:
            continue
        k, v = line.split(":", 1)
        v = v.strip()
        if v.startswith("[") and v.endswith("]"):
            meta[k.strip()] = [x.strip().strip("'\"") for x in v[1:-1].split(",") if x.strip()]
        else:
            meta[k.strip()] = v.strip().strip("'\"")
    return meta, body.lstrip("\n")


def norm(body: str) -> str:
    return re.sub(r"\s+", " ", body).strip().lower()


def sha(body: str) -> str:
    return hashlib.sha256(norm(body).encode("utf-8")).hexdigest()


def grams(s: str, n: int = 3) -> set[str]:
    s = norm(s)
    return {s[i:i + n] for i in range(max(0, len(s) - n + 1))}


def jaccard(a: set[str], b: set[str]) -> float:
    if not a or not b:
        return 0.0
    return len(a & b) / len(a | b)


def summary(body: str, n: int = 90) -> str:
    for line in body.splitlines():
        line = line.strip()
        if line and not line.startswith(("#", ">", "|", "---")):
            return line[:n]
    return ""


def collect() -> list[dict]:
    out: list[dict] = []
    for d in DOC_DIRS:
        base = KB / d
        if not base.exists():
            continue
        for p in sorted(base.rglob("*.md")):
            if p.name.startswith("_"):
                continue  # _TEMPLATE.md 之类
            text = p.read_text(encoding="utf-8", errors="replace")
            meta, body = parse_fm(text)
            eid = meta.get("id") or str(p.relative_to(KB).with_suffix("")).replace("\\", "/")
            tags = meta.get("tags") or []
            if isinstance(tags, str):
                tags = [tags]
            out.append({
                "id": eid,
                "type": meta.get("type") or d.rstrip("s"),
                "path": str(p.relative_to(KB)).replace("\\", "/"),
                "title": meta.get("title") or p.stem,
                "tags": tags,
                "source": meta.get("source", "self"),
                "license": meta.get("license", ""),
                "version": int(meta.get("version") or 1),
                "updated": meta.get("updated", ""),
                "dedupe_key": meta.get("dedupe_key", ""),
                "sha256": sha(body),
                "chars": len(body),
                "summary": summary(body),
                "_g": grams(body),
            })
    return out


def mark_dupes(entries: list[dict]) -> list[dict]:
    problems: list[dict] = []
    by_sha: dict[str, dict] = {}
    by_key: dict[str, dict] = {}
    for e in entries:
        # 精确重复
        if e["sha256"] in by_sha:
            e["duplicate_of"] = by_sha[e["sha256"]]["id"]
            problems.append({"kind": "exact", "id": e["id"], "of": e["duplicate_of"]})
            continue
        by_sha[e["sha256"]] = e
        # case 的语义键
        if e["type"] == "case" and e["dedupe_key"]:
            if e["dedupe_key"] in by_key:
                e["duplicate_of"] = by_key[e["dedupe_key"]]["id"]
                problems.append({"kind": "dedupe_key", "id": e["id"], "of": e["duplicate_of"]})
                continue
            by_key[e["dedupe_key"]] = e
    # 近似重复（同 type 内，O(n^2)，kb 规模下可接受；>5k 条改 LSH）
    for i, a in enumerate(entries):
        if a.get("duplicate_of"):
            continue
        for b in entries[i + 1:]:
            if b.get("duplicate_of") or b["type"] != a["type"]:
                continue
            if abs(len(a["_g"]) - len(b["_g"])) > 0.5 * max(len(a["_g"]), len(b["_g"]), 1):
                continue
            if jaccard(a["_g"], b["_g"]) >= NEAR_DUP:
                b["near_duplicate_of"] = a["id"]
                problems.append({"kind": "near", "id": b["id"], "of": a["id"]})
    return problems


def render_index(entries: list[dict], problems: list[dict]) -> str:
    now = datetime.now(timezone.utc).astimezone().strftime("%Y-%m-%d %H:%M %Z")
    live = [e for e in entries if not e.get("duplicate_of") and not e.get("near_duplicate_of")]
    lines = ["# 知识库索引（自动生成，勿手改）", "",
             f"- 生成时间：{now}",
             f"- 条目：{len(entries)} 条（有效 {len(live)} / 重复 {len(entries) - len(live)}）",
             f"- 字符量：{sum(e['chars'] for e in live):,}",
             "", "> 重建：`python scripts/kb_index.py`　检索：`python scripts/kb_search.py \"关键词\"`", ""]
    by_type: dict[str, list[dict]] = {}
    for e in live:
        by_type.setdefault(e["type"], []).append(e)
    for t in sorted(by_type):
        rows = by_type[t]
        lines += [f"## {t}（{len(rows)}）", "", "| id | 标签 | 摘要 |", "|---|---|---|"]
        for e in sorted(rows, key=lambda x: x["id"]):
            lines.append(f"| `{e['id']}` | {', '.join(e['tags'][:4])} | {e['summary'][:70]} |")
        lines.append("")
    if problems:
        lines += ["## 重复项（按规则应合并/删除）", "", "| 文件 | 类型 | 与谁重复 |", "|---|---|---|"]
        for p in problems:
            lines.append(f"| `{p['id']}` | {p['kind']} | `{p['of']}` |")
        lines.append("")
    return "\n".join(lines)


def main() -> int:
    entries = collect()
    problems = mark_dupes(entries)
    for e in entries:
        e.pop("_g", None)
    payload = {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "schema": 1,
        "count": len(entries),
        "valid": len([e for e in entries if not e.get("duplicate_of") and not e.get("near_duplicate_of")]),
        "problems": problems,
        "entries": entries,
    }
    idx = KB / "index.json"
    body = json.dumps(payload, ensure_ascii=False, indent=1)

    if "--check" in sys.argv:
        old = idx.read_text(encoding="utf-8") if idx.exists() else ""
        try:
            old_entries = json.loads(old).get("entries", [])
        except Exception:
            old_entries = []
        old_sig = {(e["id"], e.get("sha256")) for e in old_entries}
        new_sig = {(e["id"], e.get("sha256")) for e in entries}
        if old_sig != new_sig:
            print("[stale] index.json 与磁盘不一致，请运行: python scripts/kb_index.py")
            return 1
        print(f"[ok] index.json 是最新的（{len(entries)} 条）")
        return 0

    idx.write_text(body, encoding="utf-8")
    (KB / "INDEX.md").write_text(render_index(entries, problems), encoding="utf-8")
    print(f"[done] {len(entries)} 条 → index.json / INDEX.md；重复 {len(problems)} 项")
    for p in problems[:10]:
        print(f"  - {p['kind']}: {p['id']} ↔ {p['of']}")
    if "--strict" in sys.argv and problems:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
