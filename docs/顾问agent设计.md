# 顾问 Agent（Adviser）设计 · 全自动反跑偏

> 目标：**零经验使用者零干预**。由系统自己发现"跑偏 / 绕圈 / 只打前端"，由顾问 agent 出诊断和纠正指令，交给 planner 执行。
> 约束：**不破坏现有架构不变量** —— planner 仍然是唯一意图生成者，worker 仍然只做领到的意图。
> 本文所有挂载点均对 `RuoJi6/ARTEX` v0.3.18（commit 620ca5d）源码核对过。

---

## 一、为什么它会一直绕在前端（代码级归因）

不要归因为"模型笨"，这 5 条是结构性的：

1. **worker 被明令禁止拓宽**（`agent/worker.go` 的 worker 提示词边界第 1 条）：只做领到的一条意图，看到别的线索只能写成 fact 交给规划者。→ **planner 派不到后端，就永远没人去后端。**
2. **planner 只能看到"图里有什么"**：`graph_overview` 给的是已存在的 fact/intent/finding。**没有人给它看"图里没有什么"。** `list_untested_assets` 只在 coverage 开启时才在工具集里，且它是"资产清单"，不是"层/面清单"。
3. **没有方向级 ROI 记账**：intent 只有 done/exhausted/blocked 三个状态，**没有"这条方向累计烧了多少轮、产出了几条 fact"**。所以"同一入口换 8 种 payload"和"换一层"在 planner 眼里完全等价。
4. **禁止重派的规则反而制造了"换个说法就重来"**：`recent_done` 里出现过就不许原样重派，但换个措辞的新 intent 不违规 → 结构性鼓励"同层微变"而不是"换层"。
5. **planner 是自己结论的唯一裁判**：没有第二视角去质疑"你这 6 条 fact 全是同义复述"。

## 二、真人专家和它的行为差异

| 场景 | ARTEX 现在 | 真人专家 |
|---|---|---|
| 一个入口打不动 | 换 payload 继续 | **换维度**：换端口/换协议层/换身份/换数据流 |
| 打了 1 小时 | 不知道花了多少 | 心里有账：这条线投入产出比不对就砍 |
| 看完 web 面 | 继续在 web 面深挖 | 强制问一句："**主机层我碰过吗？数据层我碰过吗？**" |
| 得出结论 | 自己认定 | 找个人（或自己换个身份）反驳一次 |
| 目标模糊 | 按字面猜 | 先问清楚，或者先做"能改变问题定义"的那一步 |

四条行为抽象成规则：**层级配额 / ROI 记账 / 负空间优先 / 对抗式复核**。

## 三、顾问 Agent 角色定义

```
goals ──▶ planner ──▶ frontier ──▶ worker ×N ──▶ 图
            ▲                                      │
            │ add_hint（战略提示，唯一写入通道）      │
         adviser ◀────── 停顿检测器（Go，无 LLM）────┘
            │
            └─ steer_work / kill_work（对在跑的 worker）
```

- **它不派意图**（不碰 frontier），只写 `hint` + 纠偏/止损。这样去重逻辑、血缘、覆盖度全部不受影响。
- **它在 system prompt 里被定位成"资深渗透负责人"**，不是"另一个 planner"，避免它重复规划。
- **它只能看到诊断包**，不给全图（否则它跟着一起陷进去）。

## 四、唤醒条件（全部确定性计算，零 token）

`db/stall.go` 每次 planner 轮结束时算一次，任一命中即唤醒 adviser：

| 信号 | 计算 | 阈值（可配） |
|---|---|---|
| `no_progress_rounds` | 连续无新 fact 且无新 finding 的规划轮数 | ≥ 3 |
| `fact_velocity` | 最近 5 轮新 fact 数 / 5 | < 0.6 |
| `repeat_ratio` | 最近 10 条 intent 的 summary 两两 Jaccard 相似度 > 0.75 的比例 | ≥ 0.4 |
| `layer_gap` | 资产图层级有节点、但探索图对应层 fact 数为 0 的层数 | ≥ 2 |
| `surface_concentration` | 最近 8 条 intent 锚定资产中 `type='endpoint'` 占比 | ≥ 0.9 |
| `budget_burn` | 已用时长 / 任务 timeout | ≥ 0.35 且 `no_progress_rounds≥2` |

另外三个固定唤醒点：第 1 轮规划后（开局定调）、首个 finding 落库后（趁热质疑）、任务剩余预算 30% 时（收尾前最后一次换层机会）。

## 五、检测器实现

```go
// db/stall.go（新增）
type StallReport struct {
    Rounds           int      `json:"rounds"`
    NoProgressRounds int      `json:"no_progress_rounds"`
    FactVelocity     float64  `json:"fact_velocity"`
    RepeatRatio      float64  `json:"repeat_ratio"`
    LayerGap         []string `json:"layer_gap"`          // 例如 ["service","host"]
    FrontOnlyRatio   float64  `json:"front_only_ratio"`
    UntestedInScope  int      `json:"untested_in_scope"`
    CoveragePct      float64  `json:"coverage_pct"`
    BudgetBurnPct    float64  `json:"budget_burn_pct"`
    Stuck            []string `json:"stuck"`               // 命中项
}

// LayerGap 的核心 SQL：资产图有该层节点，但本任务的探索 fact 没锚到该层
//   assets.type ∈ ('root_domain','subdomain','ip','service','app','endpoint')
const qLayerGap = `
SELECT a.type, count(DISTINCT a.id)
FROM assets a
JOIN task_scope s ON s.domain = a.domain OR s.url = a.url OR s.ip = a.ip
LEFT JOIN exploration_anchors ea ON ea.asset_id = a.id AND ea.task_id = $1
WHERE a.type = ANY($2) AND ea.id IS NULL
GROUP BY a.type ORDER BY 2 DESC`
```

`RepeatRatio` 用字符 3-gram Jaccard 就够（不要引入 embedding，成本和延迟都不划算）。

## 六、顾问的输入包（诊断包，不是全图）

```
【诊断】命中：no_progress_rounds=4, repeat_ratio=0.6, layer_gap=[service,host]
【账本】本任务已跑 6 轮 / 11 条意图 / 时长 42min（占预算 58%）
        新增 fact 6 条，finding 0 个
【方向账】最近 10 条意图（摘要 + 产出 fact 数 + 是否同锚点）
        1. /login.php 弱口令枚举 → 0 fact
        2. /login.php 参数 sql 注入（布尔） → 1 fact
        3. /login.php 参数 sql 注入（时间盲注） → 0 fact      ← 与 #2 同锚点、同层
        4. /admin/ 目录探测 → 0 fact
        ...
【负空间】范围内未测资产 7 个：10.10.10.5:22(ssh) 10.10.10.5:3306(mysql) ...
        已测攻击面：web 参数 ×5、目录 ×2；未测：认证后接口、上传、SSRF、主机层
【层级足迹】host:0  service:1  endpoint:5  data:0  auth:0
【在跑的 worker】#23（/api/user?id= 越权）已跑 9 分钟，无写回
```

## 七、顾问的输出 schema（结构化，强制）

```json
{
  "diagnosis": "一句话：在后端认证面上绕了 4 轮，同锚点同层，未碰主机层与数据层",
  "evidence": ["intent#21/#23/#27 同一 endpoint", "layer host/data 的 fact 数为 0", "42min 内 0 finding"],
  "pivot": { "from": "endpoint 参数面", "to": "主机层（22/3306）+ 认证后接口", "why": "..." },
  "hints": ["停止对 /login.php 的注入尝试，已 3 次无增量", "优先验证 3306 是否可从外网直连并使用已收集到的口令", "..."],
  "steer": [{ "intent_id": 23, "message": "不要继续枚举 id，改成比对 200 与 403 的响应体差异" }],
  "kill": [27],
  "confidence": "high"
}
```

`hints` 必须**可执行**（含具体资产/端口/接口），禁止"再深入一点"这种话。

## 八、接线补丁（4 个文件 + 1 个新文件）

### 1. `db/db.go`：注册内置 agent

```go
{"adviser", "顾问", "adviser", "资深渗透负责人视角：诊断跑偏/绕圈/只打前端，出换层建议与纠偏，不派意图。",
 []promptVar{
   {"Goal", "任务总目标", "拿下 example.com 的管理员权限", "exploration"},
   {"StallReport", "停顿检测报告(JSON)", `{"no_progress_rounds":4}`, "runtime"},
 }, false, nil},
```

### 2. `agent/promptcatalog.go`：加种子

```go
"adviser": adviserDefaultTmpl,   // BuiltinPromptSeeds() 里加一行
```

### 3. `agent/adviser.go`（新文件，约 150 行，照 planner.go 裁剪）

```go
type Adviser struct { prov llm.Provider; model string; tx *transcript.Store; window, maxTurns int }

// Advise 跑一轮诊断。返回本次注入的 hint 数（0 = 它认为没事，允许空手）。
func (a *Adviser) Advise(ctx context.Context, taskID int64, as *db.AssetStore,
    ts *db.ExplorationStore, goal string, rep db.StallReport) (n int, err error) {

    tsx := NewToolSet(ts, "adviser")
    tsx.SetAssetStore(as, as.Companies())
    tsx.SetTaskID(taskID)
    base := tsx.AdviserTools()          // 读工具 + add_hint + steer_work + kill_work
    // ... 与 planner 相同的 opts 组装（PermissionMode: Bypass, BashEnv, Todos: nil）
}
```

`ToolSet.AdviserTools()`：`graph_overview / list_facts / list_findings / node_detail /
list_assets / list_untested_assets / get_worker_output / get_worker_trace /
search_all_worker_traces / add_hint / steer_work / kill_work`。
**注意：不含 `add_intent`、不含 `prove_goal`。**

### 4. `server/engine.go`：唤醒

```go
// plannerLoop 的 runRound 末尾（Plan 正常返回后）
if rep, hit := t.Store.StallReport(taskIDInt); hit && e.adviserReady(t) {
    if adv, _ := e.snapshotAdviser(t); adv != nil {
        if n, err := adv.Advise(ectx, taskIDInt, e.m.assets, t.Store, t.Goal, rep); err == nil && n > 0 {
            e.emitActivity(t, db.Activity{Worker: "adviser", Kind: "text",
                Summary: fmt.Sprintf("顾问：注入 %d 条纠正提示（%s）", n, strings.Join(rep.Stuck, ","))})
            t.Notify()   // hint 已写图，唤醒 planner
        }
    }
}
```

### 5. `server/manager.go`：构造

扩展 `Engine.SetAgentResolver` 的签名，或新增 `SetAdviserResolver(fn func(t *Task) (*agent.Adviser, error))`；
LLM profile 建议**独立配置**（顾问用强模型、worker 用便宜模型，见 §十）。

## 九、提示词（`adviserDefaultTmpl` 要点，可整段贴进 agent_prompts）

```
你是这次渗透任务的"资深负责人"。你不是规划者，也不是执行者——你只做一件事：
在团队跑偏时把它拉回来，并且只说能立刻执行的话。

任务目标：{{.Goal}}
停顿检测报告：{{.StallReport}}

【你的三条铁律】
1. 只诊断、只建议。所有动作通过 add_hint / steer_work / kill_work 发出。
   绝不自己派意图（那是规划者的职责），绝不自己跑命令。
2. 每轮最多 3 条 hint，每条必须带【具体资产/端口/接口/参数】+【为什么】+【做到什么算成功】。
   禁止"再深入一点""换个思路"这类空话——planner 无法执行它。
3. 没有真问题就不要说话。检测器误报时输出 0 条 hint 并说明"误报原因"。宁可沉默，不要干扰。

【层级配额：每次先算这张表】
host / service / endpoint / data / auth 五层，每层至少要有 1 条 fact。
哪层是 0 → 本轮 hint 必须包含一条"去碰那层"的具体动作（哪怕它看起来没戏）。
典型换层方向：endpoint 参数 → 认证后接口 / 上传 / SSRF；service → 版本对应已知漏洞的真实触发；
host → 22/3306/6379 直连与凭据复用；data → 备份文件、导出接口、日志泄露。

【ROI 记账】
同一锚点、同一手法的意图连续 2 次无新 fact → 判定该方向降权，明确写"停止 X"。
不要建议"再试一次"——除非你能说清这次和上次在机理上有什么不同。

【对抗式复核】
出现 finding 时先问一句：这条证据能不能用另一条独立路径复现？不能就建议降级为 inferred。
出现"版本匹配 CVE"类 fact 而没有实际触发 → 要求实际触发一次，否则不许进报告。

【输出】严格 JSON：diagnosis / evidence / pivot{from,to,why} / hints[] / steer[] / kill[] / confidence
```

## 十、全自动：把人审换成"顾问审 + 硬门"

三条并行，缺一不可：

1. **`intercept` 自动裁决**（替代你点批准）
   `intercept.HandleAsk` 已存在，`Reviewer` 接口已存在（`SetReviewer`，签名见 `intercept/intercept.go:121`）。
   加一个 setting `intercept_auto_decide = off|reviewer|allow_all`：`ask` 命中时先跑 Reviewer（带上任务 constraint 全文），
   返回 allow → 自动放行并落 audit（status=auto_allowed）；返回 deny → 直接拒；**只有它拿不准才挂起等你**。
   这样你不再是每步点头，而是"只在真正模糊时被叫醒"。

2. **报告硬门**（`report_finding` 必须有可复现证据）
   Intercept 规则：`tool=report_finding`，正则匹配 `evidence` 字段是否含"请求行/响应片段/命令输出"之一，不满足 → `deny` 并回话术要求补证据。
   配合 `skills/zero-to-expert` 里的证据门，双保险。

3. **复现门工具**（自定义工具，`kind=script`，不写 Go）
   `retest(finding_id)`：读 evidence 里的请求，用 curl 重放，比对响应，返回 reproduced / not_reproduced。
   挂在 worker 上，并在 skill 里规定"报 finding 前必须先过 retest"。

## 十一、落地顺序与验收

| 阶段 | 内容 | 验收 |
|---|---|---|
| P0（1 天） | 检测器 + adviser prompt + 只写 hint（不改 frontier） | 打一台 HTB Easy：跑偏 3 轮内出现"顾问：注入 N 条提示"，且随后 2 轮换层 |
| P1（2 天） | + steer/kill + ROI 记账进诊断包 | 同一入口连续 2 轮无增量后被顾问显式叫停 |
| P2（1 天） | + intercept 自动裁决（reviewer 模式） | 全程无人工点击，audit 里能看到每条自动判定与理由 |
| P3 | 独立模型 profile（顾问=强模型 / worker=便宜模型） | 同一任务成本下降 ≥ 40%，诊断质量不降 |

**验收基准**：拿 `DVWA + HTB Easy` 各 3 次跑，统计 `layer_gap 首次归零的轮数`、`finding 数`、`总 token`。
P0-P2 做完，`layer_gap` 应在 ≤ 5 轮内归零（现状往往整场都是 host:0 data:0）。
