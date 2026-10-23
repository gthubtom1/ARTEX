# `triage/` —— 从「测绘出来的一堆资产」到「值得花 AI 钱的几个任务」

这个目录补的是**两套系统之间唯一缺失的那一环**：ScopeSentry 铺出几千条资产，
ARTEX 一次只该打一个目标。中间谁来决定打哪几个？

```
ScopeSentry（测绘）                triage/                    ARTEX（深挖）
  httpx -json  ──┐
  nuclei -jsonl ─┴→ score.py ──→ shortlist.md  人看一眼
                                shortlist.jsonl ─→ dispatch.sh ─→ 任务（并发 2）
  /api/sync/scopesentry/* ────────────────────────────────────→ 资产同步（不经 triage）
```

两条通路各管一段，别混：

| 通路 | 什么时候用 | 走向 |
|---|---|---|
| **资产同步**（`deploy/wire.sh` 建 Key，ARTEX「资产同步」页触发） | 想把整张资产清单灌进 ARTEX 的资产图（`root_domain→subdomain→ip→service→app→endpoint`） | 批量、异步、可重试 |
| **`triage/`（本目录）** | 想让 AI 只打**最值的那 20 个**，其余不烧钱 | 打分 → 人工确认 → 逐条派单 |

---

## 一、打分：`score.py`

输入是**现成的产物**，不需要额外抓：

```bash
# httpx -json 的输出
cat subs.txt | httpx -json -o recon.jsonl
# nuclei 的输出
nuclei -l urls.txt -jsonl -o hits.jsonl

python3 triage/score.py --recon recon.jsonl --hits hits.jsonl --top 20 --out-prefix triage/out/w1
# 只看内网靶场（排除 127.0.0.1/localhost）
python3 triage/score.py --recon recon.jsonl --top 20 --exclude-loopback --out-prefix triage/out/lab
```

产出两份：

- `triage/out/w1.md` —— 人看的表（分数 / URL / 标题 / 理由 / 同簇数），**先看这个**；
- `triage/out/w1.jsonl` —— `dispatch.sh` 的输入。

打分规则写在文件顶部的常量里，改那里就是改你的判断标准（`HIGH_VALUE` / `SC_WEIGHT` / `PENALTY`）。
默认逻辑：高价值指纹 +30、敏感路径 +20、状态码按 200/500/302/401/403 加权，
CDN / 纯静态 / 蜜罐扣分，**同 favicon 或同指纹聚成一簇只留最高分一条**（省 90% 的 AI 成本）。

## 二、派单：`dispatch.sh`

```bash
TARGET_TIER=legacy TIMEOUT_MIN=30 bash triage/dispatch.sh triage/out/w1.jsonl 2
```

- 读 `.env` 里的 `ARTEX_PORT` / `ARTEX_PASSWORD`，先登录取 token，再 `POST /api/tasks/quickstart`；
- 并发数默认 2（第 2 个参数）；每条派过的 URL 记进 `data/dispatched.tsv`，**重跑自动跳过**；
- `127.0.0.1` / `localhost` 自动降到 `lab` 档，其余用 `TARGET_TIER`（默认 `legacy`）。

依赖：`curl` + `jq`。Windows 上在 Git Bash / WSL 里跑。

## 三、为什么不是「一个程序」

不合并的理由写在根 `README.md` 的「一个仓库、两个进程」和 `deploy/README.md`：
数据库不同（PG vs Mongo+Redis）、升级节奏不同、故障域不该合并。
这里只做**接口层的接线**，两边代码一行不改——ScopeSentry 换成 ARL 或自建管线，
`score.py` 的输入仍是 `httpx`/`nuclei` 的 JSON，本目录不用动。
