// Package impact 把"找到漏洞"翻译成"能推到哪一级"，并给出下一步要用户授权的动作。
//
// 为什么需要它（2026-10-24 用户反馈原话：「串联漏洞和线索才是渗透本质」）：
// 现在的战果只回答"发现了什么"，不回答"现在我离控制它还有几步"。
// 结果是 AI 找到一个 XSS 就收尾，用户也不知道这个洞还能干嘛。
//
// 纪律：
//  1. 等级是**规则算出来的**（可复算），不看模型自述；
//  2. 提案只描述"能做什么/风险/我会执行什么"，是否执行由用户当场决定（不预设边界）；
//  3. 不写死任何"允许到第几级"——边界永远由人在具体动作上给。
package impact

// Level 是"对这个目标已经掌握到什么程度"的阶梯。
type Level int

const (
	E0 Level = iota // 只有信息：指纹、端口、入口清单
	E1              // 有效凭据：账号/哈希/连接串（未验证可用）
	E2              // 应用控制：登录后台、越权接口、可写数据
	E3              // 命令执行：webshell / 反弹 shell / 命令回显
	E4              // 数据访问：读到业务数据（样本级）
	E5              // 服务器控制 / 内网横移
)

func (l Level) String() string {
	switch l {
	case E1:
		return "E1 有效凭据"
	case E2:
		return "E2 应用控制"
	case E3:
		return "E3 命令执行"
	case E4:
		return "E4 数据访问"
	case E5:
		return "E5 服务器控制"
	default:
		return "E0 仅有信息"
	}
}

// Stats 是算等级需要的计数（全部来自库里的既有表）。
type Stats struct {
	Findings    int // 已确认漏洞
	Creds       int // 凭据库条数
	AppControl  int // 登录后台/越权成功的证据条数
	Shells      int // 拿到了命令执行证据（webshell/反弹shell）
	DataSamples int // 读到的业务数据样本组数
	OwnedHosts  int // 持有台账里的主机数（含 root/admin）
	OwnedRoot   int // 其中 root/system 级
}

// Compute 返回当前等级与一句人话依据。
func Compute(s Stats) (Level, string) {
	switch {
	case s.OwnedRoot > 0 || s.OwnedHosts > 1:
		return E5, "持有台账里已有 root 级或跨主机立足点"
	case s.DataSamples > 0:
		return E4, "已经拿到业务数据样本"
	case s.Shells > 0:
		return E3, "已经拿到命令执行证据"
	case s.AppControl > 0:
		return E2, "已经登录进后台 / 越权访问成功"
	case s.Creds > 0:
		return E1, "凭据库里已有账号/哈希，但还没验证能登录"
	case s.Findings > 0:
		return E0, "只有漏洞，还没有把它变成任何访问权"
	default:
		return E0, "还只有侦察信息"
	}
}

// ActionClass 是需要用户当场授权的动作类别。
type ActionClass string

const (
	ClassFileWrite ActionClass = "file_write" // 上传/写文件（webshell、落地验证文件）
	ClassDataDump  ActionClass = "data_dump"  // 批量拉数据（超过样本级）
	ClassPersist   ActionClass = "persist"    // 加账号/计划任务/自启
	ClassLateral   ActionClass = "lateral"    // 向其它主机发起连接
)

// Step 是一条"下一步提案"：给零基础用户看的一屏解释 + 一个授权动作。
type Step struct {
	Class ActionClass `json:"class"`
	Title string      `json:"title"` // 做到这一步能得到什么
	Why   string      `json:"why"`   // 为什么现在能做（依据哪个漏洞）
	Plan  string      `json:"plan"`  // 授权后我会执行什么（具体动作）
	Risk  string      `json:"risk"`  // 风险 / 留痕
	Next  Level       `json:"next"`  // 做成后能升到哪一级
}

// Propose 按当前等级给出候选下一步（最多 3 条，按价值排序）。
// 关键：每条都必须能被用户"拒绝"，且拒绝后不影响其它路线。
func Propose(l Level, s Stats) []Step {
	var out []Step
	switch l {
	case E0:
		if s.Findings > 0 {
			out = append(out, Step{
				Class: ClassFileWrite, Next: E3,
				Title: "把漏洞变成命令执行（上传一个最小 webshell，验证后立即删除）",
				Why:   "已有可复现漏洞，且它可能允许写文件/上传",
				Plan:  "在已确认的上传点落一个 20 字节以内的验证文件，访问证明能执行命令，然后立刻删除并记录证据",
				Risk:  "会在目标上产生一个文件（可能被 WAF/杀软记录）；不删除会留下后门风险",
			})
			out = append(out, Step{
				Class: ClassDataDump, Next: E4,
				Title: "直接读业务数据（每类最多 3 条样本）",
				Why:   "已有可复现漏洞，可能直接读到数据",
				Plan:  "用已确认的入口读 1-3 条样本证明能读到（不整表导出、不落大盘数据）",
				Risk:  "读取行为可能被审计；样本里有真实用户数据，需要你决定能不能取",
			})
		}
	case E1:
		out = append(out, Step{
			Class: ClassLateral, Next: E2,
			Title: "用这批凭据去登录（应用后台 / 数据库 / SSH）",
			Why:   "凭据库里已有账号或哈希",
			Plan:  "先只读验证：登录后台看列表、连库 SELECT 1，成功就停，不做修改",
			Risk:  "登录失败会计入账号锁定策略；批量尝试会被封 IP",
		})
	case E2:
		out = append(out, Step{
			Class: ClassFileWrite, Next: E3,
			Title: "从后台找上传/模板/插件入口，落最小验证文件",
			Why:   "已经能控制后台，通常可以直接上传或改模板",
			Plan:  "用后台自带功能上传最小验证文件，证明能执行命令后立即删除",
			Risk:  "改配置/传文件会留下痕迹；需要你在被发现的代价上做判断",
		})
	case E3:
		out = append(out, Step{
			Class: ClassDataDump, Next: E4,
			Title: "用已有命令执行读数据库配置，取数据样本",
			Why:   "已经有命令执行，读配置与取样本是常规下一步",
			Plan:  "读数据库连接配置 → 只读取 1-3 行样本证明可读，不导出整表",
			Risk:  "会读取生产数据；日志可能记录 SQL",
		})
		out = append(out, Step{
			Class: ClassPersist, Next: E5,
			Title: "留一个可控账号（便于后续进入，退出前可删除）",
			Why:   "已经有命令执行，可加用户或计划任务",
			Plan:  "新增一个临时账号或计划任务作为入口，任务结束时删除",
			Risk:  "这条最容易被视为入侵痕迹；没有明确授权不要做",
		})
	case E4:
		out = append(out, Step{
			Class: ClassPersist, Next: E5,
			Title: "提权到 root / 系统权限",
			Why:   "能读数据说明应用层已失守，提权后能控制整机",
			Plan:  "按 GTFOBins/sudo 配置找提权路径，只做只读验证（id / whoami）",
			Risk:  "提权动作噪音大，可能触发主机防护",
		})
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}
