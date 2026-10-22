# `kb/cases/` 案例库（正案例 + 负案例）

> 一句话：**案例卡是「经验」，不是「情报」**。它回答"上次这类站是怎么打下来的 / 为什么打不下来"，
> 而不是"目标是谁、口令是什么"。
>
> 2026-10-23 起，本文件是案例库的**唯一操作说明**：
> 原 `docs/案例库自动沉淀.md` 已删除，规则收编到 `docs/00` **附录 D**（sha 去重 + 脱敏硬规则）。

---

## 一、谁写、什么时候写

| 触发 | 写入方 | 写什么 |
|---|---|---|
| 任务产生 finding（`report_finding` 落库） | `internal/cases` 自动 | 一条正案例（含证据摘要） |
| 一条方向被证死（负结论） | `internal/cases` 自动 | 一条负案例（`negative: true`） |
| 人工/AI 事后补 | 人 | 按 `_TEMPLATE.md` 填，**必须有真实证据** |

**不用新增埋点**：`exploration_nodes` / `activity` / `traffic_*` / `findings` 四张表已经齐了。
卡片的正文由 `internal/cases` 的 `render()` 生成，frontmatter **不是**手写 `dedupe_key`（见第二节）。

---

## 二、去重：**sha256 内容寻址**，不是语义键

```go
// internal/cases/cases.go:2
// Writes are content-addressed (sha256 prefix) so re-collecting the same evidence never duplicates a card.
```

- 参与哈希的字段（`canonicalCard` / `contentKey`）：`Title / Target / VulnClass / ProofLevel / Confidence /
  Summary / Evidence / Negative / Tags` → `sha256` 前 **12 位**就是文件名后缀（`YYYY-MM-DD-<12hex>.md`）。
- **同一份证据重复采集 → 命中同一路径，不产生第二张卡**（`Write` 返回 `created=false`）。
- 卡片字段（`type Card`）：`ID / Title / Target / VulnClass / ProofLevel / Confidence / Summary / Evidence / Negative / Tags`。
- 自动写入的 frontmatter 形如：

```yaml
id: task-2-finding-11
type: positive          # positive | negative
negative: false
tags: ["case", "finding", "positive", "rce", "severity:high"]
updated: 2026-10-22T10:42:51Z
target: ""
vuln_class: rce
proof_level: ""
confidence: ""
```

**已作废的旧设计（不要再实现）**：`dedupe_key = 指纹|手法|面形状|payload类`、`success_count + 1`、
`variants[]` 合并、3-gram Jaccard ≥ 0.90 自动合并、`kb/scripts/kb_case_save.py` 这个脚本工具
（`kb/scripts/` 只有 `kb_index.py` / `kb_search.py` / `kb_fetch.py`）。
`kb_index.py` 的 3-gram 打标只用于**人工确认近似重复**，不是写入路径。

---

## 三、脱敏硬规则（写死在写入路径上，不可关）

| 内容 | 处理 |
|---|---|
| 目标 IP / 域名 | 一律替换成 `TARGET` / `10.10.10.5` |
| 凭据 / Token / Cookie | 只保留"类型 + 长度"，原文丢弃 |
| 客户名 / 内网域名 | 丢弃 |
| 真实人名 / 邮箱 | 丢弃 |

**带原文的卡一律不进 `cases/`。** 复查纪律同样适用：只记"能重放的命令 + 关键响应片段"，
不记整页 HTML、不记明文口令。

---

## 四、索引更新（写入后自动）

```bash
python kb/scripts/kb_index.py            # 重建 INDEX.md / index.json
python kb/scripts/kb_index.py --check    # 索引过期返回 1（可接告警/CI）
python kb/scripts/kb_index.py --strict   # 有重复即失败（CI 用）
```

> ⚠️ 路径是 `kb/scripts/kb_index.py`。旧文档里的 `python scripts/kb_index.py` 是错的（仓库根没有 `scripts/kb_index.py`）。

---

## 五、谁会读这些卡（闭环）

- **planner**：每轮开新方向前，`agent/cases_context.go` 按目标关键词检索，**≤3 条**正/负案例注入本轮输入，
  避免重复踩已经证死的路。
- **顾问（adviser）**：出"换层建议"前应先 `kb_search` 一次该指纹——有命中就给"上次这条链怎么打的"，
  无命中说明这是**真空区**，优先级应高于重复深挖。
- **人**：任务收尾写复盘（`skills/veteran` §9），下次同指纹直接调用。

---

## 六、怎么证明"越用越强"

| 阶段 | 指标 | 观测方式 |
|---|---|---|
| 第 1 周 | cases 数量 0 → 5 | `index.json` 的 `count` |
| 第 2 周 | 新任务里检索命中旧 case 的次数 > 0 | 任务活动流里的 case 注入行 |
| 第 4 周 | 同指纹任务的「首次成功耗时」下降 | 案例卡 `updated` + 任务时长对比 |
| 长期 | 重复卡比例 < 10% | `kb_index.py --check` 的 problems |

**如果 4 周后 cases 增长但「首次成功耗时」没降，说明卡写得不合格**（缺复用条件或证据抽取失败），
回去修模板，不要继续堆数量。

---

## 七、相关文件

- 模板：`kb/cases/_TEMPLATE.md`（人工补卡时复制改名）
- 规则来源：`docs/00-总纲v2-唯一事实源.md` 附录 D（M31 行注 + 脱敏硬规则）
- 消费代码：`agent/cases_context.go`；写入代码：`internal/cases/cases.go`
