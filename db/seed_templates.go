package db

// builtinTaskTemplate is one of the task presets shipped with every fresh
// install.
//
// 2026-10-24 重做（用户实测反馈）：
//  1. 旧模板把“建议时限：20 分钟”写在 goal 文本里 —— 真正生效的是任务表单
//     「高级设置 → 超时」，文本里那句话没有任何约束力，零基础用户照着点还是默认值。
//     现在改成 TimeoutMin 字段，应用模板时直接预填表单。
//  2. 旧模板太笼统（“摸清入口，输出清单”），没有交付物判据，AI 里外都容易糊过去。
//     现在每个模板都写明：**必须产出什么**（结论/清单/证据/报告），以及边界。
//  3. 按渗透阶段排序，一个零基础用户从上往下点就行。
type builtinTaskTemplate struct {
	name        string
	description string
	goal        string
	timeoutMin  int
	stage       string // recon|surface|exploit|data|full（阶段门用）
}

// builtinTaskTemplates 是五个阶段模板：判断值不值得 → 摸入口 → 找漏洞 → 拿数据 → 出总报告。
var builtinTaskTemplates = []builtinTaskTemplate{
	{
		name:        "① 值不值得打（20 分钟）",
		description: "只读侦察后给结论：这个站硬不硬、值不值得继续投入。零基础从这里开始。",
		timeoutMin:  20,
		stage:       "recon",
		goal: `先判断这个站值不值得继续投入。只做只读侦察：域名与指纹（框架/中间件/CDN/WAF）、开放端口与服务、入口页与接口、是否全站要登录。

必须产出结论（写成一条 finding 或最后一条 fact）：
1) 值得深入 / 不值得继续，一句话理由；
2) 明显的攻击面有哪几个（带完整 URL）；
3) 有没有已知漏洞特征（版本号、已知 CVE 迹象、默认页、敏感文件）。

边界：只读。不做写操作、不爆破、不尝试绕过登录。`,
	},
	{
		name:        "② 摸入口和攻击面（30 分钟）",
		description: "把入口清单摸出来：URL / 方法 / 参数 / 表单字段 / 上传点 / 接口，每条带证据。",
		timeoutMin:  30,
		stage:       "surface",
		goal: `把这个站的入口和攻击面摸清，产出一份清单。每个入口必须写：URL、请求方法、参数或表单字段（含隐藏字段）、是否需要登录、响应特征（状态码/长度/关键字）。

覆盖范围：首页外链、所有表单、上传点、接口（/api/**、/rest/**、swagger/openapi）、管理入口、报错回显点、静态资源里的注释与路径。

边界：只读。不要提交会改数据的表单（注册/下单/改密这类只看不提交）。`,
	},
	{
		name:        "③ 找漏洞并取证（40 分钟）",
		description: "在已摸清的入口上找可复现的漏洞，每条带完整请求与响应证据。",
		timeoutMin:  40,
		stage:       "exploit",
		goal: `在已摸清的入口上找可复现的漏洞。每个漏洞必须带三样：完整请求（方法/URL/参数/必要 header）、响应里证明成功的片段（关键字或长度差异）、影响（能读到什么/能做什么）。

优先顺序：未授权访问与越权 → 注入（SQL/命令/模板）→ XSS → 任意文件读取 → 上传 → 默认口令（只试默认口令，不爆破）。

边界：只读验证。不删除、不改数据、不传 webshell；需要写入的动作先停下来问人。`,
	},
	{
		name:        "④ 能拿到什么数据（30 分钟）",
		description: "证明哪些数据实际可读（用户/订单/卡密/配置），每类最多 3 条样本。",
		timeoutMin:  30,
		stage:       "data",
		goal: `证明这个站的哪些数据实际可读：用户表、订单/支付/卡密、手机号与邮箱、配置文件与连接串。

每条证据最多取 3 行样本，写清表名与字段名；不整表导出、不落盘大量数据。

结论必须写：能拿到什么级别的后果（敏感信息泄露 / 账户接管 / 数据库访问 / 仅信息收集）。

边界：只读。任何修改类操作都不做。`,
	},
	{
		name:        "⑤ 全面评估出报告（60 分钟）",
		description: "按阶段推进（侦察→入口→漏洞→数据），最后给一份能直接给人看的结论报告。",
		timeoutMin:  60,
		stage:       "full",
		goal: `按阶段推进：侦察 → 入口 → 漏洞 → 数据影响，最后给一份结论。

报告必须包含：
1) 值得 / 不值得继续，以及硬在哪（WAF / 无攻击面 / 全站登录 / 无漏洞特征）；
2) 已确认漏洞清单，每条带证据；
3) 能拿到的数据与权限级别；
4) 还差什么（哪些面没探到、需要什么前提才能继续）。

边界：只读为主；任何写操作先停下来问人。`,
	},
}

// 旧版内置模板的 goal/描述（用于“只更新未被用户改过的那几条”）。
// 键是名字，值是当时的 goal 全文。
var legacyBuiltinTemplateGoals = map[string]string{
	"初步探测": "摸清这个站有哪些入口和攻击面，输出清单，不做任何写操作。\n\n力度：只读；建议时限：20 分钟。",
	"漏洞检测": "列出可复现的漏洞，每条带证据；不许改目标数据。\n\n力度：只读；建议时限：30 分钟。",
	"数据确认": "证明能读到哪些表/字段，每类取 3 条样本，不整表导出。\n\n力度：只读；建议时限：20 分钟。",
	"权限验证": "确认后台能否登录、服务器能否执行命令；写文件/上传前必须先问。\n\n力度：正常；建议时限：30 分钟。",
	"全面评估": "给出这个站的完整评估结论：有哪些洞、能拿到什么、还差什么。\n\n力度：正常；建议时限：60 分钟。",
}

// seedBuiltinTaskTemplates inserts the shipped presets once per database.
// Gated by a settings flag so a user's later rename/edit/delete is never
// resurrected on restart. ON CONFLICT (nkey) DO NOTHING is a second guard.
func (d *DB) seedBuiltinTaskTemplates() error {
	const flag = "task_templates_seed_v3"
	if v, _, _ := d.GetSetting(flag); v == "done" {
		return nil
	}
	for _, t := range builtinTaskTemplates {
		name := taskTemplateName(t.name)
		if _, err := d.Exec(`
INSERT INTO task_templates(name, nkey, description, goal, timeout_min, stage)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (nkey) DO NOTHING`, name, taskTemplateNKey(name), t.description, t.goal, t.timeoutMin, t.stage); err != nil {
			return err
		}
	}
	// 回填 stage：老库里这批模板已经存在（ON CONFLICT DO NOTHING 不会更新），
	// 只补 stage 仍为空的行 —— 用户自定义过的不动。
	for _, t := range builtinTaskTemplates {
		if _, err := d.Exec(`UPDATE task_templates SET stage=$1
			WHERE nkey=$2 AND coalesce(stage,'')=''`, t.stage, taskTemplateNKey(t.name)); err != nil {
			return err
		}
	}
	// 删掉被新模板取代的旧版内置条目——判据是「goal 仍是旧文本」，
	// 用户改过的一律保留（不覆盖人的判断）。
	for name, oldGoal := range legacyBuiltinTemplateGoals {
		if _, err := d.Exec(`DELETE FROM task_templates WHERE name=$1 AND goal=$2`, name, oldGoal); err != nil {
			return err
		}
	}
	if err := d.SetSetting(flag, "done"); err != nil {
		return err
	}
	return nil
}
