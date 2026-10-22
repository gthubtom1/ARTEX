# Merged Skills（外部技能包分层融入）

来源：`C:\Users\toti\Desktop\Pentest-Skills-Merged`（236 个 agentskills.io 规范技能）。
导入脚本：`python scripts/import_merged_skills.py [源目录]`（可重复执行，会覆盖同名目录）。

## 三层处理，避免"变强但变卡"

| 层 | 内容 | 位置 | 是否进提示词 |
|---|---|---|---|
| 知识层 | **138 个 license=MIT** 的技能全文 | `kb/external/merged-skills/<name>/SKILL.md` | 否，`kb_search`/知识库页按需检索 |
| 执行层 | **8 个精选 MIT 技能** | `skills/<name>/`（norma Skill 工具按需加载） | 只有 name+description（默认绑 worker/pentest） |
| 排除层 | 98 个 license 缺失/非 MIT（多为超大 Windows 课程） | 不提交；仅列在 `EXCLUDED.md` | 否 |

精选（执行层）：`hunt-api-misconfig`、`hunt-auth-bypass`、`401-403-bypass`、`hunt-idor`、
`hunt-file-upload`、`hunt-race-condition`、`docker-privesc`、`evidence-hygiene`。

## 为什么不全量注册成 Skill

`Skill` 工具会把每个可见技能的 name+description 放进系统提示词。236 个全开会给每轮
planner/worker 增加约 1.5–2 万 token，直接拖慢任务并推高成本；而且多数 `offensive-*`
大课程与 ARTEX 的 Web/AD 主线无关。因此：
- 广度靠**检索**（kb_search 命中原技能全文，含 payload/命令）；
- 深度靠**少量精选**（worker 执行时按需加载步骤）；
- planner 不绑技能，保持上下文稳定与缓存命中。

## 覆盖增益（相对现有 kb）

新增/补强的面：API 误配（mass assignment/JWT/原型污染）、认证绕过、401/403 绕过、
IDOR、上传→RCE、竞态、Docker 提权、证据打码与报告卫生，以及 bug-bounty 侦察工作流。
与现有 `kb/external/cyberskills-index.json`（818 条索引，仅元数据）**零重名**，是纯增量。

## 许可与维护

- 执行层只复制 `license: MIT` 的技能；知识层同样只收 MIT；未标注许可的一律不提交。
- 需要新增/移除精选技能：改 `scripts/import_merged_skills.py` 的 `CURATED` 后重跑，
  再在 `db/db.go` 的 `builtinSkillVisibility` 增删绑定（或在前端 Agent 管理里手动开关）。
