# 基准跑批结果（`bench/results/`）

这里每次跑批写一个 JSON：`<run_key>.json`。**后端只读不写**，脚本（`scripts/bench/run.py`）
写，`GET /api/bench/run?key=...` 读。schema 与 `internal/bench/bench.go` 的结构体一一对应，
改名/加字段必须两边同时改，否则环比会静默失真。

## run_key 规则

```
run_key = YYYYMMDD-HHMM-<set>-<modelslug>
```

- 时间取跑批开始时刻（服务器本地时区），分钟精度，例如 `20261022-1530`。
- `<set>`：`--set` 名字（`smoke` / `basic` / `dvwa` / `dsvw` / `juice` / `all`），
  非 `[A-Za-z0-9._-]` 的字符替换为 `-`，连续 `-` 折叠，上限 24 字符。
- `<modelslug>`：`model_profile` 的短名，净化规则同上，上限 **32 字符**（`internal/bench.BuildRunKey`）。
- 结果文件名 = `<run_key>.json`，且必须满足 `^[A-Za-z0-9._-]+\.json$` 且不含 `..`
  （`internal/bench.SafeRunKey`，后端读文件前必须调它）。
- 同一分钟内、同 set、同模型重跑会撞名 —— 这是有意的：撞名就等于「重跑覆盖上次」，
  想留两份就换 set 名或等一分钟。

## JSON schema

```json
{
  "run_key": "20261022-1530-basic-flash",
  "set_name": "basic",
  "model_profile": "deepseek-v4.1-flash",
  "started_at": "2026-10-22T15:30:00+08:00",
  "total": 10,
  "passed": 4,
  "duration_ms": 1830000,
  "total_tokens": 412000,
  "cases": [
    {
      "key": "sqli-01",
      "category": "sqli",
      "passed": true,
      "duration_ms": 96000,
      "first_finding_ms": 41200,
      "tokens": 31000,
      "task_id": "42",
      "outcome": "passed",
      "note": "命中 vulnclass=sql-injection"
    }
  ]
}
```

### 顶层字段

| 字段 | 类型 | 含义 |
|---|---|---|
| `run_key` | string | 主键，也是文件名（不含 `.json`）；非空。 |
| `set_name` | string | 用例集名（`--set` 原值），环比只在同 `set_name` 内比。 |
| `model_profile` | string | `llm_profile_id` 对应的模型短名（解析不到时退化为 id 字符串）。**两次结果可比的前提之一**。 |
| `started_at` | string | 跑批开始时刻，RFC3339（带本地时区偏移）。 |
| `total` | int | 本次用例数。 |
| `passed` | int | 通过数，必须 `<= total`。 |
| `duration_ms` | int64 | 整批墙钟耗时（毫秒）。 |
| `total_tokens` | int64 | 所有用例 `tokens` 之和。 |
| `cases` | array | 逐题结果；`total > 0` 时长度必须等于 `total`。 |

### case 字段

| 字段 | 类型 | 含义 |
|---|---|---|
| `key` | string | 用例 key（= `bench/cases/<key>.yaml` 文件名）。非空。 |
| `category` | string | `sqli` / `cmdi` / `path-traversal` / `upload` / `deser` / `ssrf` / `idor` / `auth-bypass` / `ssti` / `weak-pass` / `xss`。 |
| `passed` | bool | 是否命中该用例的 `pass` 规则。 |
| `duration_ms` | int64 | 该题从建任务到进入终态（或跑批侧超时）的墙钟耗时。 |
| `first_finding_ms` | int64 | 该题**首个 finding** 的 `ts` 相对任务开始时间的毫秒数；没有 finding 记 `0`。 |
| `tokens` | int64 | 该任务 `tokens.input_tokens + tokens.output_tokens`（不含 cache 读写）。 |
| `task_id` | string | 平台任务 id（便于人工回看/清理）；建任务失败时为空。 |
| `outcome` | string | 枚举：`passed` \| `failed` \| `timeout` \| `error`。`passed=true` 时必须是 `passed`。 |
| `note` | string | 人读备注：失败原因、命中的 vulnclass、拉 findings 失败等。 |

### outcome 语义

- `passed`：拿到至少一条 finding，且**所有非空判据同时满足**（vulnclass 大小写不敏感子串 AND
  surface 正则 AND evidence 正则）。
- `failed`：任务正常结束（`done`/`failed`），但没有 finding 满足判据。
- `timeout`：跑批侧等待超过 `timeout_s + timeout_pad`，或平台把任务置为 `timeout`。
- `error`：平台/网络错误（建任务失败、5xx、响应不可解析等），本题不计入通过。

## 汇总与环比

`internal/bench.Summarize`：

- `pass_rate = passed / total`，**0..1 的比例**（前端展示时 ×100）。
- `avg_duration_ms = sum(case.duration_ms) / len(cases)`。
- `avg_first_finding_ms` 只对 `first_finding_ms > 0` 的用例求均值（没出洞的题不该被算成 0ms）。

`internal/bench.Delta(prev, cur)`：

- `delta_pass_rate = (cur.pass_rate - prev.pass_rate) × 100`，单位**百分点（pp）**。
- `delta_duration_ms = cur.avg_duration_ms - prev.avg_duration_ms`，单位毫秒（负数 = 更快）。
- 没有上一次同 set+model 的结果时，`delta_*` 恒为 `0`（不是"退步"）。

## 两次结果可比的前提

只有下面三条同时成立，环比才有意义；否则请当成两次独立的观测：

1. **同 `set_name`**：题目集合、题面、`pass` 规则都没变（改了 YAML 就等于换了尺子）。
2. **同 `model_profile`**：同一个模型/同一份 profile 配置（温度、思考档、上下文窗口都算配置）。
3. **同平台版本**：同一次构建（`git rev-parse HEAD` / 镜像 tag 一致），
   并且靶场（`bench/compose.yml`）的镜像版本一致。

补充：`--limit` 截断后的结果 `total` 会变小，**不要**拿它跟全量结果比通过率 —— 要么都用全量，
要么两次用同样的 `--limit` 与同样的用例顺序（脚本按文件名排序取前 N 个）。

## 其它

- 这些用例打的全是**本地构造靶场**（DVWA / Juice Shop / DSVW），只绑 `127.0.0.1`。
  这里的通过率**不代表**平台对真实站点的能力，汇报时不要混为一谈。
- 结果 JSON 只用于展示与环比，不存请求/响应原文；证据留在平台的 finding 里，用 `task_id` 回看。
- 手工重放登记：`POST /api/bench/ingest`（只接受顶层汇总字段，幂等 UPSERT by `run_key`）。
