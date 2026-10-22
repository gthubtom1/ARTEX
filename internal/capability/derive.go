package capability

import (
	"sort"
	"strings"
)

// Card 是一张能力卡。字段与 db.capabilities 表一一对应。
type Card struct {
	ID             int64   `json:"id,omitempty"`
	TaskID         int64   `json:"task_id,omitempty"`
	Group          int     `json:"group"`
	Item           string  `json:"item"`
	Can            string  `json:"can"`
	Plain          string  `json:"plain,omitempty"`
	Surface        string  `json:"surface"`
	Evidence       string  `json:"evidence"`
	ProofLevel     string  `json:"proof_level"`
	Confidence     string  `json:"confidence"` // observed|inferred
	Impact         string  `json:"impact,omitempty"`
	Risk           string  `json:"risk,omitempty"`
	NextIfApproved string  `json:"next_if_approved,omitempty"`
	SourceNodeIDs  []int64 `json:"source_node_ids,omitempty"`
	Status         string  `json:"status,omitempty"` // open|approved|declined|done
	Value          string  `json:"value,omitempty"`
}

// Signal 是派生能力卡的输入（来自 finding / fact / 手工声明）。
type Signal struct {
	Item       string  `json:"item"` // 直接指定清单编号（推荐；为空时用 Classify 猜）
	Kind       string  `json:"kind"` // finding|fact|data|cred|shell|privesc|lateral|cloud
	Text       string  `json:"text"` // 原始摘要（用于猜编号 + 兜底 can 文案）
	Surface    string  `json:"surface"`
	Evidence   string  `json:"evidence"`
	ProofLevel string  `json:"proof_level"`
	Confidence string  `json:"confidence"`
	Impact     string  `json:"impact"`
	Risk       string  `json:"risk"`
	Next       string  `json:"next_if_approved"`
	NodeIDs    []int64 `json:"source_node_ids"`
}

// Derive 把一批信号派生成能力卡：查清单补全文案、按 (组,项,入口) 去重、
// 证明级别取最高、按组优先级排序。未识别编号的信号不会丢——落到"组 0 未分类"。
func Derive(sigs []Signal) []Card {
	return DeriveWithTaxonomy(Default(), sigs)
}

// DeriveWithTaxonomy 是 Derive 的可注入版本（便于测试）。
func DeriveWithTaxonomy(t *Taxonomy, sigs []Signal) []Card {
	type key struct {
		item    string
		surface string
	}
	best := map[key]*Card{}
	var extra []Card // 未分类
	for _, s := range sigs {
		item := strings.TrimSpace(s.Item)
		if item == "" {
			item, _ = Classify(s.Text)
		}
		tx, ok := t.Get(item)
		if !ok {
			c := Card{
				Group: 0, Item: item, Can: firstNonEmpty(s.Text, "未分类能力"),
				Surface: s.Surface, Evidence: s.Evidence,
				ProofLevel: normProof(s.ProofLevel), Confidence: normConf(s.Confidence),
				Impact: s.Impact, Risk: s.Risk, NextIfApproved: s.Next,
				SourceNodeIDs: s.NodeIDs, Status: "open",
			}
			extra = append(extra, c)
			continue
		}
		k := key{item: tx.Item, surface: strings.TrimSpace(s.Surface)}
		c := Card{
			Group: tx.Group, Item: tx.Item, Can: tx.Can, Plain: tx.Plain,
			Surface: s.Surface, Evidence: s.Evidence,
			ProofLevel: normProof(s.ProofLevel), Confidence: normConf(s.Confidence),
			Impact: s.Impact, Risk: s.Risk, NextIfApproved: s.Next,
			SourceNodeIDs: s.NodeIDs, Status: "open", Value: tx.Value,
		}
		if prev, ok := best[k]; ok {
			// 同一 (项, 入口)：证明级别取高，证据拼接，节点合并。
			if ProofRank(c.ProofLevel) > ProofRank(prev.ProofLevel) {
				prev.ProofLevel = c.ProofLevel
			}
			if prev.Evidence == "" {
				prev.Evidence = c.Evidence
			} else if c.Evidence != "" && !strings.Contains(prev.Evidence, c.Evidence) {
				prev.Evidence = prev.Evidence + "；" + c.Evidence
			}
			if prev.Impact == "" {
				prev.Impact = c.Impact
			}
			if prev.NextIfApproved == "" {
				prev.NextIfApproved = c.NextIfApproved
			}
			prev.SourceNodeIDs = mergeIDs(prev.SourceNodeIDs, c.SourceNodeIDs)
			continue
		}
		best[k] = &c
	}
	out := make([]Card, 0, len(best)+len(extra))
	for _, c := range best {
		out = append(out, *c)
	}
	out = append(out, extra...)

	// 排序：组号越小越值钱（数据组最前）；同组按证明级别降序，再按编号。
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := out[i].Group, out[j].Group
		if gi == 0 {
			gi = 99
		}
		if gj == 0 {
			gj = 99
		}
		if gi != gj {
			return gi < gj
		}
		if ProofRank(out[i].ProofLevel) != ProofRank(out[j].ProofLevel) {
			return ProofRank(out[i].ProofLevel) > ProofRank(out[j].ProofLevel)
		}
		return out[i].Item < out[j].Item
	})
	return out
}

// normProof 把非法/空级别归一到 P1（只读证明）——绝不把"没证据"写成 P2+。
func normProof(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if !ValidProof(s) {
		return "P1"
	}
	return s
}

func normConf(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "observed", "inferred":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return "inferred"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func mergeIDs(a, b []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range append(append([]int64{}, a...), b...) {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// classifyRules 把常见漏洞/事实关键词映射到清单编号。
// 顺序有意义：先匹配更具体的规则（例如"反序列化 RCE"要落在 4.5 而不是 4.1）。
var classifyRules = []struct {
	item string
	kws  []string
}{
	{"4.5", []string{"反序列化", "deserial", "gadget"}},
	{"4.4", []string{"ssti", "模板注入", "template injection"}},
	{"4.3", []string{"上传", "upload"}},
	{"4.8", []string{"无回显", "带外", "oast", "blind rce"}},
	{"4.2", []string{"反弹 shell", "reverse shell", "交互 shell", "getshell"}},
	{"4.1", []string{"命令执行", "rce", "command injection", "代码执行"}},
	{"5.1", []string{"文件读取", "任意文件读", "lfi", "fileread", "path traversal read"}},
	{"5.2", []string{"文件写入", "任意文件写", "arbitrary write"}},
	{"5.3", []string{"目录遍历", "directory listing", "dir traversal"}},
	{"1.5", []string{".git", ".svn", ".bak", "备份", "源码泄露", "db.sql", "www.zip"}},
	{"1.7", []string{"oss", "s3", "bucket", "对象存储", "存储桶"}},
	{"1.8", []string{"redis", "elasticsearch", "未授权缓存", "_cat/indices"}},
	{"1.10", []string{"exchange", "ews", "邮件接口"}},
	{"1.1", []string{"information_schema", "count(*)", "表结构", "数据库结构", "行数"}},
	{"1.2", []string{"越权", "idor", "换 id", "水平越权"}},
	{"1.3", []string{"未授权访问", "unauthorized", "未授权列表"}},
	{"1.4", []string{"导出越权", "分页越权", "size=10000", "/export"}},
	{"2.1", []string{"明文口令", "明文密码", "hardcoded password"}},
	{"2.2", []string{"哈希", "ntlm", "hashdump", "密码哈希"}},
	{"2.3", []string{"jwt", "cookie", "session token", "会话令牌"}},
	{"2.4", []string{"api key", "apikey", "ak/sk", "accesskey", "secretkey"}},
	{"2.5", []string{"ssh 私钥", "id_rsa", "private key"}},
	{"2.6", []string{"连接串", "connection string", "jdbc", "mysql://", "postgres://"}},
	{"2.7", []string{"管理员登录", "后台登录", "admin login"}},
	{"2.9", []string{"密码重置", "password reset"}},
	{"2.10", []string{"凭据复用", "密码复用", "password reuse", "撞库成功"}},
	{"3.1", []string{"进入后台", "管理面板", "admin panel"}},
	{"3.2", []string{"升管理员", "提权到管理员", "mass assignment", "role=admin"}},
	{"3.3", []string{"跨租户", "tenant", "多租户越权"}},
	{"3.4", []string{"认证绕过", "auth bypass", "绕过登录"}},
	{"6.8", []string{"云元数据", "169.254.169.254", "metadata service", "iam 凭据"}},
	{"6.7", []string{"容器逃逸", "docker socket", "k8s", "kubernetes", "特权容器"}},
	{"6.3", []string{"内核漏洞", "kernel exploit"}},
	{"6.2", []string{"sudo", "suid", "capability 提权", "提权"}},
	{"7.2", []string{"隧道", "代理", "pivot", "tunnel", "socks"}},
	{"7.5", []string{"smb", "winrm", "wmi", "横向移动", "lateral"}},
	{"7.6", []string{"数据库横向", "db pivot"}},
	{"7.1", []string{"内网可达", "ip route", "内网段"}},
	{"8.1", []string{"webshell", "内存马"}},
	{"9.1", []string{"改价", "退款", "余额", "1 分钱"}},
	{"9.2", []string{"优惠券", "积分", "coupon"}},
	{"9.3", []string{"订单篡改", "库存", "order tamper"}},
	{"9.4", []string{"越权操作", "改密码", "解绑"}},
	{"9.5", []string{"批量注册", "验证码绕过"}},
	{"10.1", []string{"云控制台", "ak sk 接管"}},
	{"10.5", []string{"rbac", "secret 读取"}},
	{"11.1", []string{"smtp", "开放中继", "open relay"}},
	{"11.2", []string{"owa", "exchange 接管"}},
	{"12.3", []string{"更新通道", "update hijack"}},
}

// Classify 用关键词把一段描述映射到清单编号。
func Classify(text string) (string, bool) {
	low := strings.ToLower(text)
	for _, r := range classifyRules {
		for _, kw := range r.kws {
			if strings.Contains(low, strings.ToLower(kw)) {
				return r.item, true
			}
		}
	}
	return "", false
}
