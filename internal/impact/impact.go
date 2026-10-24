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

import "strings"

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
	OwnedHosts  int // 持有台账里的条目数
	OwnedRoot   int // 其中"操作系统级"的 root/system（access_type 为 shell/ssh/agent）
	OwnedApp    int // 应用层控制（access_type 为 admin/web/session，例如登录进后台）
}

// Compute 返回当前等级与一句人话依据。
func Compute(s Stats) (Level, string) {
	switch {
	// 教训（2026-10-24 实战验收）：台账里 access_type=admin 是"应用后台管理员"，
	// 被当成"服务器 root"直接跳到 E5 —— 等级虚高比不显示更糟。
	// 现在只有"操作系统级立足点"才算 E5；应用后台属于 E2。
	case s.OwnedRoot > 0 || s.OwnedHosts > 1:
		return E5, "已有操作系统级立足点（root/system）或跨主机控制"
	case s.DataSamples > 0:
		return E4, "已经拿到业务数据样本"
	case s.Shells > 0:
		return E3, "已经拿到命令执行证据"
	case s.OwnedApp > 0 || s.AppControl > 0:
		return E2, "已经登录进后台 / 越权访问成功（应用层控制）"
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
	// Precondition：这一步的前置能力（人话）。前置没满足就不该"现在做"。
	Precondition string `json:"precondition"`
	// Ready：前置是否已满足（确定性判定，不靠模型猜）。
	// 未满足时界面只能显示"还差：<前置>"，不允许显示成"它下一步想做"。
	Ready bool `json:"ready"`
}

// Propose 按当前等级给出候选下一步（最多 3 条，按价值排序）。
// 关键：每条都必须能被用户"拒绝"，且拒绝后不影响其它路线。
//
// 2026-10-24 修正（用户指出）：E0（只有漏洞、还没拿到任何权限）原来直接建议
// "上传一个最小 webshell" —— 那是拿到上传/写文件能力之后才该做的事，
// 在 E0 提这个等于让模型跳过"验证可达性"直接去落文件，既不合逻辑也不安全。
// 现在 E0 只给"验证这个洞能不能换来东西"的只读步骤，并且按漏洞类型分派。
//
// kinds 是已确认漏洞的类型/标题（小写拼起来的判定见 kindHas）。
func Propose(l Level, s Stats, kinds []string) []Step {
	var out []Step
	switch l {
	case E0:
		if s.Findings == 0 {
			return nil
		}
		// 只有**确认了上传/写文件类**入口，才允许提"落最小验证文件"。
		if kindHas(kinds, "上传", "写文件", "upload", "webshell", "put") {
			out = append(out, Step{
				Class: ClassFileWrite, Next: E2,
				Title:        "用已确认的上传/写入点落一个最小验证文件（验证后立即删除）",
				Why:          "已经确认存在上传或写文件入口，落文件是把它变成能力的下一步",
				Plan:         "只落一个不含后门的最小验证文件，访问证明可执行后立刻删除并记录证据",
				Risk:         "会在目标上产生一个文件（可能被杀软/WAF 记录）；不删除会留下后门风险",
				Precondition: "已确认的上传/写入入口", Ready: true,
			})
		}
		// 其余情况：只读验证可达性（读 1 条、进一次、不落文件）。
		out = append(out, Step{
			Class: ClassDataDump, Next: E2,
			Title:        "只读验证：证明这个洞能真的读到东西（1 条即可，不落文件）",
			Why:          "已确认漏洞，但还没证明它能换来访问或数据；先做最小的只读验证",
			Plan:         "用已确认入口读回 1 条可控数据（或进入一次后台页面），成功即停，不做任何写操作",
			Risk:         "读取行为可能被审计；样本可能含真实用户数据，由你决定取不取",
			Precondition: "已确认漏洞（需先验证可达性）", Ready: true,
		})
	case E1:
		out = append(out, Step{
			Class: ClassDataDump, Next: E2,
			Title:        "用这批凭据去登录验证（只读，失败即停）",
			Why:          "凭据库里已有账号或哈希，但还没验证能不能用",
			Plan:         "先只读验证：登录后台看列表 / 连库 SELECT 1，成功就停，不做修改",
			Risk:         "登录失败会计入账号锁定策略；批量尝试会被封 IP",
			Precondition: "凭据库里有账号或哈希（已满足）", Ready: true,
		})
	case E2:
		out = append(out, Step{
			Class: ClassFileWrite, Next: E3,
			Title:        "从后台找上传/模板/插件入口，落最小验证文件",
			Why:          "已经能控制后台，通常可以直接上传或改模板",
			Plan:         "用后台自带功能上传最小验证文件，证明能执行命令后立即删除",
			Risk:         "改配置/传文件会留下痕迹；需要你在被发现的代价上做判断",
			Precondition: "应用层控制（已进后台/越权成功）", Ready: s.OwnedApp > 0 || s.AppControl > 0,
		})
		if s.OwnedApp == 0 && s.AppControl == 0 {
			// 兜底：理论上到不了这里（E2 就代表应用控制），但别让 Ready 撒谎。
			out[0].Ready = true
		}
	case E3:
		out = append(out, Step{
			Class: ClassDataDump, Next: E4,
			Title:        "用已有命令执行读数据库配置，取数据样本",
			Why:          "已经有命令执行，读配置与取样本是常规下一步",
			Plan:         "读数据库连接配置 → 只读 1-3 行样本证明可读，不导出整表",
			Risk:         "会读取生产数据；日志可能记录 SQL",
			Precondition: "命令执行（已满足）", Ready: s.Shells > 0,
		})
		out = append(out, Step{
			Class: ClassPersist, Next: E5,
			Title:        "留一个可控账号（便于后续进入，退出前可删除）",
			Why:          "已经有命令执行，可加用户或计划任务",
			Plan:         "新增一个临时账号或计划任务作为入口，任务结束时删除",
			Risk:         "这条最容易被视为入侵痕迹；没有明确授权不要做",
			Precondition: "命令执行（已满足）", Ready: s.Shells > 0,
		})
	case E4:
		out = append(out, Step{
			Class: ClassPersist, Next: E5,
			Title:        "提权到 root / 系统权限",
			Why:          "能读数据说明应用层已失守，提权后能控制整机",
			Plan:         "按 GTFOBins/sudo 配置找提权路径，只做只读验证（id / whoami）",
			Risk:         "提权动作噪音大，可能触发主机防护",
			Precondition: "已能读到业务数据（已满足）", Ready: s.DataSamples > 0,
		})
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// kindHas 判断已确认漏洞里有没有提到这些特征（小写比较，中文原样）。
func kindHas(kinds []string, subs ...string) bool {
	blob := strings.ToLower(strings.Join(kinds, " | "))
	for _, sub := range subs {
		if sub != "" && strings.Contains(blob, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
