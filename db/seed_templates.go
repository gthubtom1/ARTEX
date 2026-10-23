package db

import "fmt"

// builtinTaskTemplate is one of the task presets shipped with every fresh
// install. task_templates has no strength/timeout columns, so the operating
// posture and time budget are carried in the goal text (the text a new task
// gets verbatim, and the one planner/worker actually read).
type builtinTaskTemplate struct {
	name        string
	description string
	goal        string
}

// builtinTaskTemplates are the five starting points a new machine gets on first
// launch. Users only need to paste a target URL into the task form; the
// description/goal already spell out scope and rules of engagement.
var builtinTaskTemplates = []builtinTaskTemplate{
	{
		name:        "初步探测",
		description: "对目标做只读侦察：指纹、端口、目录、接口清单，不出结论、不动手。",
		goal:        "摸清这个站有哪些入口和攻击面，输出清单，不做任何写操作。\n\n力度：只读；建议时限：20 分钟。",
	},
	{
		name:        "漏洞检测",
		description: "找漏洞并逐个验证，只做只读验证，每个漏洞带可复现请求。",
		goal:        "列出可复现的漏洞，每条带证据；不许改目标数据。\n\n力度：只读；建议时限：30 分钟。",
	},
	{
		name:        "数据确认",
		description: "确认能否读到它的数据，只取少量样本证明。",
		goal:        "证明能读到哪些表/字段，每类取 3 条样本，不整表导出。\n\n力度：只读；建议时限：20 分钟。",
	},
	{
		name:        "权限验证",
		description: "验证能否登录后台或拿到系统权限，落地类动作先问人。",
		goal:        "确认后台能否登录、服务器能否执行命令；写文件/上传前必须先问。\n\n力度：正常；建议时限：30 分钟。",
	},
	{
		name:        "全面评估",
		description: "按阶段推进：侦察→漏洞→数据→权限，按预算分配时间。",
		goal:        "给出这个站的完整评估结论：有哪些洞、能拿到什么、还差什么。\n\n力度：正常；建议时限：60 分钟。",
	},
}

// seedBuiltinTaskTemplates inserts the five shipped presets once per database.
// Gated by a settings flag so a user's later rename/edit/delete is never
// resurrected on restart. ON CONFLICT (nkey) DO NOTHING is a second guard: if
// the flag was lost but a same-name preset already exists, its contents (which
// the user may have edited) are left untouched.
func (d *DB) seedBuiltinTaskTemplates() error {
	const flag = "task_templates_seed_v1"
	if v, _, _ := d.GetSetting(flag); v == "done" {
		return nil
	}
	for _, t := range builtinTaskTemplates {
		name := taskTemplateName(t.name)
		if _, err := d.Exec(`
INSERT INTO task_templates(name, nkey, description, goal)
VALUES ($1, $2, $3, $4)
ON CONFLICT (nkey) DO NOTHING`,
			name, taskTemplateNKey(name), t.description, t.goal); err != nil {
			return fmt.Errorf("task template %q: %w", t.name, err)
		}
	}
	return d.SetSetting(flag, "done")
}
