# CARD-E01 机制生效保证（L1 提示 + 度量口径）

> 先读：`docs/10-机制生效保证.md`
> 依赖：无

## ① 目标
让"该用工具的活真的用上工具"：先做 L1（每请求注入的工具优先纪律），并把度量口径写死在文档里，达不到就逐级升到 L2/L3。

## ② 为什么
实测：系统发现 8422 个工具、worker 绑定 45 个内置工具（含 nmap_scan/httpx_probe/nuclei_scan，均非延迟加载），但 45 分钟里 `httpx_probe`/`nuclei_scan`/`ffuf`/`whatweb`/`hydra` **各 0 次**，`curl` 178 次。**提示词里没有"该用什么"，模型就一律手搓。**

## ③ 必读事实
1. 注入层（`internal/inject/persona_default.go`）是**每请求现取**，改完下一轮即生效 —— 这是唯一能立刻影响已有装机的提示通道（agent_prompts 是版本化种子，老装机不会自动更新）。
2. 结构化工具已注册且 worker 可见，不需要新增工具。
3. 手写不是错：注入/文件读取/带内回显用 curl 更精确 —— 纪律只针对"端口/指纹/目录/口令"四类。

## ④ 改哪
- 修改：`internal/inject/persona_default.go`（加【工具优先】一节）
- 新增：`docs/06-任务卡/CARD-E01.md`（本卡）

## ⑤ 怎么改
在【语言】之前插入【工具优先】：点名四类活 → 四个结构性工具；说明手写仍适用于注入类；给一句判断标准。

## ⑥ 白名单
```whitelist
internal/inject/persona_default.go
docs/06-任务卡/CARD-E01.md
```

## ⑦ 禁止
```forbidden
agent/tools.go
server/cando.go
server/review.go
web/src/app/(main)/function/capabilities/page.tsx
```

## ⑧ 验收
```
go build ./... && go test ./internal/inject/ -count=1
bash scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-E01.md
```
G5（下一批真实任务）：`nmap_scan` 占端口扫描类调用 ≥50%；`httpx_probe` ≥1；`ffuf` ≥1（有目录线索时）。达不到 → 按 docs/10 升 L2（guard 命令改道）。

## ⑨ 已知坑
1. 注入层是"每请求现取"，改完**必须重启**才加载新二进制里的常量。
2. 不要写"禁止使用 nmap 命令"这类否定句——会反向激活规则脑补；写成"先用结构化工具"。
3. 现有装机不会自动更新 agent_prompts 版本，别把期望放在那里。

## ⑩ 停止条件
若注入层被 `inject.preset=off` 关闭 → 停下回报：需要在设置里开启，或改走 agent_prompts 迁移。

## ⑪ 完成回执
diff、两条命令输出、下一批任务的工具调用分布（SQL：按 summary 首词统计）。
