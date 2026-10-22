# ARTEX 二开说明（本仓库 = gthubtom1 的长期资产）

- 上游：https://github.com/RuoJi6/ARTEX （AGPL-3.0，本仓库保留原 LICENSE 与版权声明）
- 定位：在原作者 planner/worker/双图/记录代理架构上做**增强层**，不重写运行时。
- 主要新增：能力卡与决策队列、三门校验、凭据库/立足点池/持有台账、复查执行器、顾问/校验角色、
  注入层（on-miss/on-refusal）、KB MCP、真回放、数据定位、案例沉淀、自动裁决、10 个前端页面。
- 兼容：新功能以新增包/新路由/新页面为主；原页面与核心语义未改。改动清单见 `docs/进度台账.md`、
  `docs/P2-P5-实现说明.md`、`docs/P6-P7-实现说明.md`。
- 运行：需要 PostgreSQL + 一个 OpenAI 兼容模型端点；`skills/` 与 `kb/` 需与二进制同目录（或配置
  `ARTEX_SKILL_DIR` / `kb.path`）。前端已内嵌，可用 `scripts/package_local.sh` 打包。
- 上游同步：`git fetch upstream && git merge upstream/main`（fork 保留 upstream 远端）。
