// Package chain 把「一条漏洞走到哪一步」算成一条链：确认 → 授权 → 收益。
//
// 用户判定（原话）：「串联漏洞和线索才是渗透本质」。
// 之前每个漏洞是孤立节点，没地方回答"这条洞走到哪、卡在哪、拿到了什么"；
// 这里用纯函数把它算出来（可单测），由 server 落库（可跨会话累计）。
package chain

import "sort"

// FindingRef 是一条已确认漏洞。
type FindingRef struct {
	ID    int64
	Title string
}

// GrantRef 是用户在即时授权卡上的决定。
type GrantRef struct {
	Class   string
	Granted bool
}

// GainRef 是一条"真的拿到了"的证据：凭据/命令执行/数据/立足点。
type GainRef struct {
	Kind string // cred|shell|data|owned
	Ref  int64
}

// Step 是链上的一步。
type Step struct {
	Key    string
	Seq    int
	Kind   string // entry|authorize|gain
	Title  string
	Status string // done|pending|failed
	RefID  int64
}

// Chain 是一条利用链。
type Chain struct {
	Key    string
	Title  string
	Status string // open|done|dead
	Steps  []Step
}

// Build 按确定性规则生成链：同样输入永远同样输出。
//
// 规则：
//   - 一条漏洞一条链（key=F<id>）；
//   - 第 1 步：确认（done）；
//   - 第 2 步：把洞变成收益需要的授权（file_write/data_dump/lateral），
//     未决定=pending，允许=done，拒绝=failed；
//   - 第 3..n 步：实际拿到的收益（cred/shell/data/owned），done；
//   - 有 exec/数据/立足点收益 → 链 status=done；所有授权都被拒 → dead。
func Build(findings []FindingRef, grants []GrantRef, gains []GainRef) []Chain {
	grantState := map[string]string{}
	for _, g := range grants {
		if g.Granted {
			grantState[g.Class] = "done"
		} else {
			grantState[g.Class] = "failed"
		}
	}
	out := make([]Chain, 0, len(findings))
	for _, f := range findings {
		c := Chain{Key: "F" + itoa(f.ID), Title: f.Title, Status: "open"}
		c.Steps = append(c.Steps, Step{
			Key: "confirm", Seq: 0, Kind: "entry",
			Title: "确认漏洞：" + f.Title, Status: "done", RefID: f.ID,
		})

		// 需要的授权（按收益方向选一个；两个都需要时按顺序排）
		needed := []string{"file_write", "data_dump"}
		anyPending, allFailed := false, true
		seq := 1
		for _, cls := range needed {
			st, ok := grantState[cls]
			if !ok {
				anyPending = true
				allFailed = false
				st = "pending"
			} else if st == "done" {
				allFailed = false
			}
			c.Steps = append(c.Steps, Step{
				Key: "authorize:" + cls, Seq: seq, Kind: "authorize",
				Title: "授权：" + classCN(cls), Status: st,
			})
			seq++
		}

		// 已拿到的收益（全局收益按漏洞都算进来：单任务内收益是共享的）
		for _, g := range gains {
			c.Steps = append(c.Steps, Step{
				Key: "gain:" + g.Kind, Seq: seq, Kind: "gain",
				Title: "拿到：" + gainCN(g.Kind), Status: "done", RefID: g.Ref,
			})
			seq++
			if g.Kind == "shell" || g.Kind == "data" || g.Kind == "owned" {
				c.Status = "done"
			}
		}
		if c.Status != "done" && allFailed && !anyPending {
			c.Status = "dead"
		}
		sort.SliceStable(c.Steps, func(i, j int) bool { return c.Steps[i].Seq < c.Steps[j].Seq })
		out = append(out, c)
	}
	return out
}

func classCN(c string) string {
	switch c {
	case "file_write":
		return "上传/写文件（webshell、落地验证文件）"
	case "data_dump":
		return "批量取数据"
	case "lateral":
		return "向其它主机发起连接"
	case "persist":
		return "留可控账号/计划任务"
	}
	return c
}

func gainCN(k string) string {
	switch k {
	case "cred":
		return "有效凭据"
	case "shell":
		return "命令执行"
	case "data":
		return "业务数据样本"
	case "owned":
		return "立足点（台账）"
	}
	return k
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
