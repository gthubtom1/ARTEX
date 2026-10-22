# ARTEX 知识库（kb/）

> 一个**可长期更新、自动去重、可被 agent 按需检索**的经验库。
> 原则：**正文不进提示词**，只通过 `kb_search` 拿摘要 + 按 path 读原文。这是省 token 且不腐坏上下文的唯一方式。

## 目录

| 目录 | 放什么 | 谁写 |
|---|---|---|
| `tactics/` | 每种手法（sqli/auth/upload/ssrf/deser/rce/privesc…）的**检查顺序 + 判定标准 + 最小验证** | 人 / AI 补充 |
| `payloads/` | 每种手法的**最小可用 payload 集**（3–5 条，永不收全库） | 人 / AI 补充 |
| `surfaces/` | 接口/参数**形状 → 该查哪些类**；攻击类总表 | 脚本生成 + 人补充 |
| `fingerprints/` | 技术栈指纹 → 已知漏洞族 + 默认口令 + 默认路径；CWE 映射 | 人 / AI 补充 |
| `cases/` | **案例卡**：一次任务成功/失败的真实记录（自动沉淀） | ARTEX 自动写 |
| `sources/` | 外部来源登记（网址/许可/抓取时间/状态）——**只登记，不整包下载** | 脚本 / 人 |
| `prompts/` | 补充知识库用的**可移植提示词**（给任何 AI 用） | 人 |
| `scripts/` | `kb_index.py` 建索引、`kb_search.py` 检索、`kb_fetch.py` 抓取 | 人 |
| `inbox/` | AI 抓回来的原始物，**未归档**（不参与索引，人工确认后移出） | AI |
| `index.json` | 机器索引（唯一事实源） | `kb_index.py` |
| `INDEX.md` | 人类可读索引 | `kb_index.py` |

## 三条硬规则

1. **一切条目必须带 frontmatter**（`id/type/tags/source/license/version/updated`），否则 `kb_index.py` 会归到默认值，检索会漏。
2. **重复只留一份**：精确重复（内容 sha256 相同）与近似重复（3-gram Jaccard ≥ 0.90）由 `kb_index.py` 标出，人确认后删；case 另有语义键 `dedupe_key` 去重。
3. **不整包下载大库**：nuclei/SecLists 这类只登记到 `sources/`，用时按 path 取。`kb/` 目标体积 < 50MB。

## 日常流程

```bash
# 补知识（人或任何 AI 按 prompts/00-补充知识库.md 执行）→ 产出落到 inbox/ 或对应目录
python scripts/kb_index.py          # 重建索引（会报重复）
python scripts/kb_index.py --check   # CI/定时任务：索引是否过期
python scripts/kb_search.py "thinkphp 5.0 rce" -n 3

# 从外网补料（GitHub API 未认证只有 60 次/小时，批量时先设 token）
export GITHUB_TOKEN=ghp_xxx
python scripts/kb_fetch.py probe                       # 来源探活
python scripts/kb_fetch.py list projectdiscovery/nuclei-templates http/technologies
python scripts/kb_fetch.py get  swisskyrepo/PayloadsAllTheThings "SQL Injection/README.md"
```

## 与 ARTEX 的接法

三个自定义工具（`系统 → Tools`，`kind = script`，脚本目录指向 `kb/scripts/`）：

| 工具 | 入参 | 出参 |
|---|---|---|
| `kb_search` | `{"query","limit","type","tags"}` | `{"hits":[{id,path,score,summary}]}` |
| `kb_read` | `{"id"}` | 正文（由 agent 用 Read 按 path 读，工具只回 path） |
| `kb_case_save` | 案例卡字段 | `{"id","dedupe":"new|merged"}` |

配一个 skill：**什么时候必须查（写下含 version 的 fact 时 / 看到一个 endpoint 形状时 / 确定手法时）**，查不到就老实说查不到，别编。
