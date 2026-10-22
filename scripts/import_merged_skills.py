#!/usr/bin/env python3
"""把外部 Pentest-Skills-Merged 技能包分层融入 ARTEX。

分层策略（避免 token 与许可证风险）：
  1) 知识层：只收录 license=MIT 的技能全文 → kb/external/merged-skills/<name>/SKILL.md
     供 kb_search / 知识库页按需检索（不进系统提示词）。
  2) 执行层：精选一小批与 ARTEX 能力卡/CWE 覆盖面直接相关的 MIT 技能 → skills/<name>/
     由 norma Skill 工具按需加载（默认绑定 worker/pentest，见 db/db.go）。
  3) 不收录：license 缺失/非 MIT 的技能（多为超大 Windows 课程），只在 EXCLUDED.md 列名，
     避免把来源不明的长文提交进仓库。

用法：python scripts/import_merged_skills.py [源目录]
默认源目录：C:/Users/toti/Desktop/Pentest-Skills-Merged
"""
import json
import re
import shutil
import sys
from pathlib import Path

SRC = Path(sys.argv[1] if len(sys.argv) > 1 else r"C:\Users\toti\Desktop\Pentest-Skills-Merged")
ROOT = Path(__file__).resolve().parent.parent
KB_OUT = ROOT / "kb" / "external" / "merged-skills"
SKILLS_OUT = ROOT / "skills"

CURATED = [
    "hunt-api-misconfig",
    "hunt-auth-bypass",
    "401-403-bypass",
    "hunt-idor",
    "hunt-file-upload",
    "hunt-race-condition",
    "cloud-iam-deep",
    "docker-privesc",
    "evidence-hygiene",
]


def frontmatter(text: str) -> dict:
    if not text.startswith("---"):
        return {}
    body = text.split("---", 2)[1]
    out = {}
    for line in body.splitlines():
        m = re.match(r"^([a-zA-Z_]+):\s*(.*)$", line)
        if m:
            out[m.group(1)] = m.group(2).strip().strip('"\'')
    return out


def copy_dir(src: Path, dst: Path):
    if dst.exists():
        shutil.rmtree(dst)
    shutil.copytree(src, dst)


def main():
    if not SRC.is_dir():
        raise SystemExit(f"源目录不存在：{SRC}")
    KB_OUT.mkdir(parents=True, exist_ok=True)

    included, excluded = [], []
    for d in sorted(SRC.iterdir()):
        if not d.is_dir():
            continue
        skill = d / "SKILL.md"
        if not skill.exists():
            continue
        text = skill.read_text(encoding="utf-8", errors="ignore")
        fm = frontmatter(text)
        name = d.name
        meta = {
            "name": name,
            "description": fm.get("description", ""),
            "category": fm.get("category", ""),
            "license": fm.get("license", ""),
            "version": fm.get("version", ""),
            "bytes": len(text.encode("utf-8")),
        }
        if fm.get("license") != "MIT":
            excluded.append(meta)
            continue
        copy_dir(d, KB_OUT / name)
        included.append(meta)

    # 知识层索引（人读 + 机器读）
    lines = [
        "# Merged Skills 知识层（MIT）",
        "",
        "> 由 `scripts/import_merged_skills.py` 从外部 Pentest-Skills-Merged 导入。",
        "> 只收录 license=MIT 的技能全文；**不进系统提示词**，由 `kb_search` / 知识库页按需检索。",
        "> 执行层精选见仓库 `skills/` 下同名目录（默认绑定 worker/pentest）。",
        "",
        f"- 收录：**{len(included)}** 个（MIT）",
        f"- 未收录：**{len(excluded)}** 个（license 缺失/非 MIT，见 `EXCLUDED.md`）",
        "",
        "| 技能 | 分类 | 字节 | 描述（截断） |",
        "|---|---|---|---|",
    ]
    for m in sorted(included, key=lambda x: (-x["bytes"], x["name"])):
        desc = (m["description"] or "").replace("|", "\\|")
        if len(desc) > 180:
            desc = desc[:180] + "…"
        lines.append(f"| `{m['name']}` | {m['category'] or '-'} | {m['bytes']} | {desc} |")
    (KB_OUT / "INDEX.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    (KB_OUT / "manifest.json").write_text(
        json.dumps({"included": included, "excluded": excluded}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    ex = ["# 未收录（license 缺失/非 MIT）", "", "这些技能保留在外部源目录，不提交进仓库；需要时由用户手工放入 `kb/`。", ""]
    for m in sorted(excluded, key=lambda x: x["name"]):
        ex.append(f"- `{m['name']}` — {m['category'] or '-'}（{m['bytes']} 字节）")
    (KB_OUT / "EXCLUDED.md").write_text("\n".join(ex) + "\n", encoding="utf-8")

    # 执行层：精选 MIT 技能复制到 skills/（norma 只认 SKILL.md 目录）
    copied = []
    for name in CURATED:
        src = SRC / name
        if not (src / "SKILL.md").exists():
            print(f"[!] 精选技能缺失：{name}")
            continue
        if frontmatter((src / "SKILL.md").read_text(encoding="utf-8", errors="ignore")).get("license") != "MIT":
            print(f"[!] 精选技能不是 MIT，跳过：{name}")
            continue
        copy_dir(src, SKILLS_OUT / name)
        copied.append(name)

    print(f"知识层：{len(included)} 个 → {KB_OUT}")
    print(f"未收录：{len(excluded)} 个 → {KB_OUT / 'EXCLUDED.md'}")
    print(f"执行层：{len(copied)} 个 → {SKILLS_OUT}: {', '.join(copied)}")


if __name__ == "__main__":
    main()
