package db

// builtinTaskTemplate 是一个内置任务模板。
//
// 2026-10-24 重写（用户反馈：零基础不知道该选哪个、该写什么）：
//  1. description 用一句话说清"这轮要拿到什么"，零基础直接选；
//  2. goal 按老手的实际打法写成有序步骤（先做什么→看到什么→拿到什么算完成），
//     不再是一句笼统的"摸清入口";
//  3. timeoutMin 是真实时长（应用模板时预填「高级设置 → 超时」），不再写在文本里；
//  4. stage 供阶段门使用（recon 阶段未出结论前禁利用类命令）。
//  5. 越界动作一律写明"由平台向你请求授权"，不预设边界。
type builtinTaskTemplate struct {
	name        string
	description string
	goal        string
	timeoutMin  int
	stage       string
	// isPrimary：只把 3 个"开局"放主视线（docs/25 §六）；其余 5 个降级到"更多"。
	isPrimary bool
}

// builtinTaskTemplates 按"一次渗透的真实顺序"排列，从上往下选即可。
var builtinTaskTemplates = []builtinTaskTemplate{
	{
		name:        "① 值不值得打（20 分钟）",
		description: "先花 20 分钟只读侦察，给出结论：这个站值不值得继续投入、硬在哪。零基础从这里开始。",
		timeoutMin:  20,
		stage:       "recon",
		goal: `这一轮只做一件事：判断这个站值不值得继续投入。全程只读，任何利用动作都不要做。

按这个顺序做：
1. 解析：域名/IP、证书、CDN 或 WAF（有就别急着打，先记下来）。
2. 端口与服务：只扫常见端口 + 全端口快速探活，记录开放端口与指纹。
3. 入口页：首页、登录、注册、后台、上传、搜索、API 文档（swagger/actuator/openapi）、robots、sitemap。
4. 快速判定攻击面：有没有未授权接口、有没有登录面、有没有明显旧版本特征。

必须产出结论（写成一条 finding 或最后一条 fact）：
- 值得深入 / 不值得继续，一句话理由；
- 攻击面清单（带完整 URL，按价值排序）；
- 如果"不值得"，写清硬在哪（WAF / 全站登录 / 无动态参数 / 纯静态）。

边界：只读。不爆破、不尝试绕过登录、不写任何东西。`,
	},
	{
		name:        "② 摸入口和攻击面（30 分钟）",
		description: "把入口摸全：URL、方法、参数、表单字段、上传点、接口，每条带响应特征。为下一步找洞做准备。",
		timeoutMin:  30,
		stage:       "surface",
		goal: `目标：把这个站的入口和攻击面摸全，产出一份可以直接拿去打的清单。

按这个顺序做：
1. 爬一遍站内链接与目录（不爆破，只做常见路径 + 首页外链）。
2. 每个页面提取：表单（action/method/全部字段含隐藏字段）、参数、上传点、富文本/编辑器。
3. 接口面单独拉一遍：/api/**、/rest/**、/v1/**、graphql、swagger、actuator、.well-known。
4. 记录响应特征：状态码、长度、类型、是否回显入参、是否有调试信息/堆栈。
5. 标记"疑似未授权"（无 cookie 直接 200 且返回业务数据）与"疑似注入点"（参数进 SQL/命令/模板）。

必须产出：入口清单（每条：URL / 方法 / 参数或字段 / 是否需要登录 / 响应特征 / 风险标记）。

边界：只读。不要提交会改数据的表单（注册、下单、改密这类只看不提交）。`,
	},
	{
		name:        "③ 找漏洞并取证（40 分钟）",
		description: "在入口清单上找可复现漏洞，每条带完整请求+响应证据；并至少把一条往上推到命令执行或数据访问。",
		timeoutMin:  40,
		stage:       "exploit",
		goal: `目标不是"列出漏洞"，而是至少把一条漏洞变成实际的命令执行或数据访问。

第一段——找洞并取证（每条都必须带证据）：
1. 未授权访问与越权（水平/垂直）优先，最容易直接拿到数据。
2. 注入：SQL（报错/布尔/时间）、命令、模板、LDAP；每个注入点做一次对照（去掉 payload 再发一遍）。
3. 文件读取 / 路径穿越 / 源码泄露（.git、.env、备份）。
4. 上传点：类型/后缀/内容校验，先只测能不能传一个无害文件（不落 webshell）。
5. XSS：优先能打管理员的存储型；反射型要证明能触达后台。
6. 默认口令（只试出厂默认，不爆破）。

第二段——往上推一步（挑最有希望的一条）：
- 有注入 → 读库表名 + 取 ≤3 条样本；或写文件拿命令回显；
- 有上传 → 传最小验证文件，证明能执行命令后立刻删除；
- 有后台 → 找上传/模板/插件入口，按上面同样方式验证；
- 做到就把结果用 owned_register / record_fact 登记（等级才会涨）。

边界：上传/写文件、批量取数据、留账号、横移这些动作，平台会向你请求授权，按提示停下来等人确认。`,
	},
	{
		name:        "④ 能拿到什么数据（30 分钟）",
		description: "证明哪些数据实际可读：用户/订单/卡密/配置；每类最多 3 条样本，写清表名字段名。",
		timeoutMin:  30,
		stage:       "data",
		goal: `目标：证明这个站的真实数据能被读到，并写清"读到什么级别"。

按这个顺序做：
1. 用已有入口定位数据库：读配置文件/环境变量/连接串，或直接从注入点读库结构。
2. 列出能读到的表与关键字段（用户、订单、支付、卡密、手机号、邮箱、日志）。
3. 每类最多取 3 行样本作为证据，写清表名与字段名；不整表导出、不落盘大量数据。
4. 用 record_fact 写回"读到了哪张表哪些字段"，战果页会自动标成 ✓ 已拿到。
5. 结论写：能拿到什么级别的后果（敏感信息泄露 / 账户接管 / 数据库访问 / 仅信息收集），以及"再往下一步需要什么"。

边界：只读。批量导出、写文件、留账号由平台向你请求授权后再说。`,
	},
	{
		name:        "⑤ 全面评估出报告（60 分钟）",
		description: "按阶段推进（侦察→入口→漏洞→数据→权限），最后给一份能直接交给客户看的报告。",
		timeoutMin:  60,
		stage:       "full",
		goal: `目标：给出这个站的完整评估结论，并产出一份可交付报告。

按这个顺序做：
1. 侦察与入口（可复用②的产出，没有就先快速补）。
2. 漏洞：按价值排序，每条带完整请求 + 响应证据 + 影响。
3. 权限：尝试把最有希望的一条推到登录/命令执行/数据样本；做到就登记。
4. 报告必须包含：
   - 值得 / 不值得继续，以及硬在哪；
   - 已确认漏洞清单（每条带证据）；
   - 能拿到的数据与权限级别；
   - 还差什么（哪些面没探到、需要什么前提）。

边界：只读为主；任何写操作先停下来问人。`,
	},
	{
		name:        "⑥ 后台接管（30 分钟）",
		description: "专攻登录面：默认口令/注入/越权进后台，进去后立刻取证并找上传点。",
		timeoutMin:  30,
		stage:       "exploit",
		goal: `目标：进后台，并证明进去以后能做什么。

按这个顺序做：
1. 找全部登录入口：/admin、/login、/manage、/console、/druid、/phpmyadmin、端口管理面（8080/8443/9000）。
2. 逐条尝试（每条只验一次，不爆破）：
   - 出厂默认口令（admin/admin、admin/123456、admin/admin888…）；
   - 登录处 SQL 注入（' or 1=1--、admin'--）；
   - 越权：改 role/userid 参数、去掉鉴权头、IDOR 换 id。
3. 进后台后立刻取证：截图/请求响应、可见的菜单与数据列表、版本信息。
4. 在后台找能落文件的地方：上传、模板编辑、插件安装、数据导入、备份恢复。
5. 用 owned_register 登记（access_type=admin，写清是哪个后台、怎么进的）。

必须产出：进入方式（可复现请求）+ 后台能做什么 + 是否能落文件。

边界：能落文件的动作先只做"能不能传"的探测，真的要传由平台向你请求授权。`,
	},
	{
		name:        "⑦ 命令执行与提权（40 分钟）",
		description: "已经从漏洞拿到命令执行后，读配置、找库、查提权路径，把权限往上顶。",
		timeoutMin:  40,
		stage:       "exploit",
		goal: `前提：已经有命令执行（webshell / 反弹 shell / RCE）。目标：把它变成"能控制这台机器"。

按这个顺序做（一遍走完，别跳）：
1. 确认身份与系统：id、whoami、uname -a、hostname、cat /etc/os-release。
2. 读配置找连接串：.env、config.php、application.yml、wp-config.php、环境变量、~/.bash_history。
3. 找数据库并用配置里的凭据只读连接，取 ≤3 条样本证明可读。
4. 提权检查清单：sudo -l、SUID（find / -perm -4000）、定时任务、可写服务配置、内核版本对应 CVE。
5. 有内网再考虑横移：ip a / route、内网可达段、代理工具。
6. 每拿到一步就用 owned_register / record_fact 登记（等级才会涨）。

必须产出：当前权限级别、可用凭据、提权路径（能成就做，不能成就写清卡在哪）。

边界：提权、留账号、横移这些动作由平台向你请求授权，按提示停下等人确认；不要留后门。`,
	},
	{
		name:        "⑧ 复测与收尾（20 分钟）",
		description: "漏洞修没修？用原请求再打一遍，给出「已修复 / 仍可利用」的结论，并整理报告。",
		timeoutMin:  20,
		stage:       "surface",
		goal: `目标：验证之前的漏洞现在还在不在，并给出复测结论。

按这个顺序做：
1. 取出每个已确认漏洞的原始请求（方法/URL/参数/header）。
2. 原样重放一次；再做一次"去掉 payload"的对照。
3. 判定：仍可利用 / 已修复 / 无法判断（说清为什么）。
4. 仍可利用的，尝试是否还能扩大影响（读数据/执行命令），但不要重复利用。
5. 产出复测结论表：漏洞 / 原请求 / 现在结果 / 结论。

边界：只读；不要为了复测去改数据。`,
	},
}

// ── ①③⑤ 的三段落文本（docs/25 §六）──────────────────────────────────────
//
// 模板文本的三条硬规矩（"写得越全 ≠ 越好"的落点）：
//  1. 只写"看到什么算有"，**不写"这个站应该有 X"** —— 否则模型会照着"应该有"去找不存在的洞；
//  2. 每一项都要给"没有就记为不适用，别硬凑"的出口；
//  3. 必须写"什么时候停"（硬判据），避免没希望的站跑十几个小时烧 token。
var primaryTemplateOverrides = map[string]struct {
	goal      string
	oldPrefix string
}{
	"① 值不值得打（20 分钟）": {
		oldPrefix: "先判断这个站值不值得继续投入",
		goal: `【这一轮干什么】只读侦察 20 分钟，只有一个产出：这个站值不值得继续投入。

【看到什么算"有"】（没有就把这一项记为"不适用"，别硬凑）
- 有登录面：登录/注册/后台入口返回 200，或 403/401（403 也说明存在）。
- 有动态入口：URL 带参数、有表单、有 /api 或 swagger/actuator 文档。
- 有旧版本特征：响应头/静态文件名/报错页里出现明确版本号。
- 有防护：WAF / CDN / 全站登录 —— 这也是结论，写清硬在哪。

【什么时候必须停】（满足任一条就停，并写一条 fact 或 finding 作结论）
- 已经能明确回答"值得 / 不值得"；
- 20 分钟到；
- 连续两个动作没有任何新信息（同一批端口/路径重复出现）。

边界：只读。不爆破、不试默认口令、不写任何东西。`,
	},
	"③ 找漏洞并取证（40 分钟）": {
		oldPrefix: "目标不是",
		goal: `【这一轮干什么】在入口清单上找可复现漏洞，并至少把一条往上推一步（能读数据或能进后台）。

【看到什么算"有"】（只认证据，不认"应该存在"）
- 注入：同一条请求去掉 payload 再发一遍，响应有可对比差异（报错/时间/内容）。
- 越权：换 id 或换角色后，返回了不属于当前账号的数据。
- 未授权：不带 token 访问接口，返回业务数据而不是 401/403。
- 上传：传一个无害文件后能访问到它（先不要落 webshell）。
- 泄露：.git / .env / 备份 / 源码可下载，且内容里出现凭据或配置项。
以上任一条成立，就用 finding 落证据（附完整请求 + 响应）。

【什么时候必须停】
- 已推进一步（读到数据 / 进后台 / 命令回显）→ 停，把结果登记（等级才会涨）；
- 40 分钟到；
- 连续两个动作无新证据 → 停，写清"卡在哪、缺什么"。

边界：上传/写文件、批量取数、留账号、横移，平台会向你请求授权后再做。`,
	},
	"⑤ 全面评估出报告（60 分钟）": {
		oldPrefix: "按阶段推进",
		goal: `【这一轮干什么】给出完整结论 + 一份能直接交付的报告。

【看到什么算"有"】（报告里每条都要挂证据，没有证据的不写）
- 结论：值得 / 不值得，以及硬在哪；
- 漏洞清单：每条带完整请求与响应片段；
- 权限与数据：到哪一级（E0–E5）、能读到哪些表哪些字段；
- 缺口：哪些面没探到、需要什么前提。

【什么时候必须停】
- 报告要素齐了 → 停（不要为了"再多找一个洞"继续烧钱）；
- 60 分钟到；
- 连续两次无新证据 → 用当前材料出报告。

边界：只读为主；任何写操作先停下来问人。`,
	},
}

// 老版本 ③④ 的 goal 全文（用于"只升级未被用户改过的那两条"）。
const (
	oldGoalExploit = `在已摸清的入口上找可复现的漏洞。每个漏洞必须带三样：完整请求（方法/URL/参数/必要 header）、响应里证明成功的片段（关键字或长度差异）、影响（能读到什么/能做什么）。

优先顺序：未授权访问与越权 → 注入（SQL/命令/模板）→ 任意文件读取 → 上传 → 默认口令（只试默认口令，不爆破）。

边界：只读验证。不删除、不改数据、不传 webshell；需要写入的动作先停下来问人。`

	oldGoalData = `证明这个站的哪些数据实际可读：用户表、订单/支付/卡密、手机号与邮箱、配置文件与连接串。

每条证据最多取 3 行样本，写清表名与字段名；不整表导出、不落盘大量数据。

结论必须写：能拿到什么级别的后果（敏感信息泄露 / 账户接管 / 数据库访问 / 仅信息收集）。

边界：只读。任何修改类操作都不做。`
)

// 旧版内置模板的名字（用于"未被用户改过就替换"）。
var legacyBuiltinTemplateNames = []string{
	"初步探测", "漏洞检测", "数据确认", "权限验证", "全面评估",
}

// seedBuiltinTaskTemplates inserts the shipped presets once per database.
// Gated by a settings flag so a user's later rename/edit/delete is never
// resurrected on restart.
func (d *DB) seedBuiltinTaskTemplates() error {
	// 主模板升级独立于"首次播种"：老库里 8 条早就存在，seed 会因 flag 直接返回，
	// 所以这里必须单独跑一次（自己的 flag），否则 ①③⑤ 永远升不到三段落文本。
	if err := d.upgradePrimaryTaskTemplates(); err != nil {
		return err
	}
	const flag = "task_templates_seed_v5"
	if v, _, _ := d.GetSetting(flag); v == "done" {
		return nil
	}
	for _, t := range builtinTaskTemplates {
		name := taskTemplateName(t.name)
		if _, err := d.Exec(`
INSERT INTO task_templates(name, nkey, description, goal, timeout_min, stage, is_primary)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (nkey) DO NOTHING`, name, taskTemplateNKey(name), t.description, t.goal, t.timeoutMin, t.stage, isPrimaryFor(t.name)); err != nil {
			return err
		}
	}
	// 回填 ③④ 的 goal（仍是旧文本才改，用户改过的不动）。
	for i, oldGoal := range map[int]string{2: oldGoalExploit, 3: oldGoalData} {
		if i >= len(builtinTaskTemplates) {
			continue
		}
		t := builtinTaskTemplates[i]
		if _, err := d.Exec(`UPDATE task_templates SET goal=$1, description=$2, updated_at=now()
			WHERE nkey=$3 AND goal=$4`, t.goal, t.description, taskTemplateNKey(t.name), oldGoal); err != nil {
			return err
		}
	}
	// 回填 stage / timeout：老库里这几条已存在，ON CONFLICT 不会更新。
	for _, t := range builtinTaskTemplates {
		if _, err := d.Exec(`UPDATE task_templates SET stage=COALESCE(NULLIF(stage,''), $1),
			timeout_min=CASE WHEN timeout_min=0 THEN $2 ELSE timeout_min END
			WHERE nkey=$3`, t.stage, t.timeoutMin, taskTemplateNKey(t.name)); err != nil {
			return err
		}
	}
	if err := d.SetSetting(flag, "done"); err != nil {
		return err
	}
	return nil
}

// isPrimaryFor 标记哪几个是"开局"模板（其余进"更多"）。
func isPrimaryFor(name string) bool {
	_, ok := primaryTemplateOverrides[name]
	return ok
}

// upgradePrimaryTaskTemplates：把 ①③⑤ 升成"三段落文本 + 主模板"，其余 5 个降级为"更多"。
// 只在老文本还没被用户改过时覆盖（前缀匹配）；单独 flag 保证只跑一次。
func (d *DB) upgradePrimaryTaskTemplates() error {
	const primaryFlag = "task_templates_primary_v2"
	if v, _, _ := d.GetSetting(primaryFlag); v == "done" {
		return nil
	}
	for _, t := range builtinTaskTemplates {
		nkey := taskTemplateNKey(t.name)
		if _, err := d.Exec(`UPDATE task_templates SET is_primary=$1 WHERE nkey=$2`,
			isPrimaryFor(t.name), nkey); err != nil {
			return err
		}
		if o, ok := primaryTemplateOverrides[t.name]; ok {
			if _, err := d.Exec(`UPDATE task_templates SET goal=$1, description=$2, updated_at=now()
				WHERE nkey=$3 AND goal LIKE $4`, o.goal, t.description, nkey, o.oldPrefix+"%"); err != nil {
				return err
			}
		}
	}
	return d.SetSetting(primaryFlag, "done")
}
