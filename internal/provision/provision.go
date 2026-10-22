// Package provision 负责"缺什么工具、用什么方式装、装不上用什么替代"。
//
// 只做规划（Plan），不直接执行：真正的安装由上层按 Step.Command 执行，
// 这样便于审计、dry-run 与人工确认。
package provision

import (
	"fmt"
	"sort"
	"strings"
)

// Method 是安装方式。
type Method string

const (
	MethodAPT     Method = "apt"
	MethodPipx    Method = "pipx"
	MethodGo      Method = "go"
	MethodRelease Method = "release" // GitHub Releases（需 sha256 校验）
	MethodDocker  Method = "docker"
	MethodManual  Method = "manual"
)

// Spec 描述一个工具的安装与降级方式。
type Spec struct {
	Tool        string
	APT         string   // apt 包名
	Pipx        string   // pipx 包名
	Go          string   // go install 路径
	ReleaseRepo string   // owner/repo（GitHub Releases）
	Verify      string   // 安装后自检命令（如 "nmap --version"）
	Fallback    []string // 装不上时的等价替代
	Risk        string   // low|medium|high（high 需人工确认）
}

// Registry 是工具注册表（按需扩充；未知工具走 manual）。
var Registry = map[string]Spec{
	"nmap":        {Tool: "nmap", APT: "nmap", Verify: "nmap --version", Fallback: []string{"masscan+nc"}, Risk: "low"},
	"masscan":     {Tool: "masscan", APT: "masscan", Verify: "masscan --version", Fallback: []string{"nmap -sT"}, Risk: "low"},
	"ffuf":        {Tool: "ffuf", Go: "github.com/ffuf/ffuf/v2@latest", Verify: "ffuf -V", Fallback: []string{"gobuster", "feroxbuster", "curl 并发"}, Risk: "low"},
	"gobuster":    {Tool: "gobuster", APT: "gobuster", Verify: "gobuster version", Fallback: []string{"ffuf"}, Risk: "low"},
	"feroxbuster": {Tool: "feroxbuster", APT: "feroxbuster", Verify: "feroxbuster --version", Fallback: []string{"ffuf"}, Risk: "low"},
	"nuclei":      {Tool: "nuclei", Go: "github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest", Verify: "nuclei -version", Fallback: []string{"curl + 本地 PoC"}, Risk: "low"},
	"sqlmap":      {Tool: "sqlmap", APT: "sqlmap", Pipx: "sqlmap", Verify: "sqlmap --version", Fallback: []string{"内置注入脚本(curl 布尔/时间)"}, Risk: "medium"},
	"hydra":       {Tool: "hydra", APT: "hydra", Verify: "hydra -h", Fallback: []string{"netexec --proto ssh"}, Risk: "medium"},
	"john":        {Tool: "john", APT: "john", Verify: "john --list=build-info", Fallback: []string{"hashcat"}, Risk: "low"},
	"chisel":      {Tool: "chisel", ReleaseRepo: "jpillora/chisel", Verify: "chisel --version", Fallback: []string{"ssh -D", "ligolo-ng", "socat"}, Risk: "low"},
	"ligolo-ng":   {Tool: "ligolo-ng", ReleaseRepo: "nicocha30/ligolo-ng", Verify: "ligolo-ng -h", Fallback: []string{"chisel", "ssh -D"}, Risk: "low"},
	"proxychains4": {Tool: "proxychains4", APT: "proxychains4", Verify: "proxychains4 -h", Fallback: []string{"ALL_PROXY 环境变量"}, Risk: "low"},
	"sshpass":     {Tool: "sshpass", APT: "sshpass", Verify: "sshpass -V", Fallback: []string{"expect"}, Risk: "low"},
	"netexec":     {Tool: "netexec", Pipx: "netexec", Verify: "netexec --version", Fallback: []string{"impacket 全套"}, Risk: "medium"},
	"impacket":    {Tool: "impacket", APT: "python3-impacket", Verify: "impacket-psexec -h", Fallback: []string{"netexec"}, Risk: "medium"},
	"smbclient":   {Tool: "smbclient", APT: "smbclient", Verify: "smbclient --version", Fallback: []string{"netexec smb"}, Risk: "low"},
	"ldapsearch":  {Tool: "ldapsearch", APT: "ldap-utils", Verify: "ldapsearch -VV", Fallback: []string{"ldapdomaindump"}, Risk: "low"},
	"jq":          {Tool: "jq", APT: "jq", Verify: "jq --version", Fallback: []string{"python3 -m json.tool"}, Risk: "low"},
}

// Step 是一条可执行的安装动作。
type Step struct {
	Tool    string `json:"tool"`
	Method  Method `json:"method"`
	Command string `json:"command"`
	Note    string `json:"note,omitempty"`
}

// Plan 为缺失工具生成安装计划（去重、按工具名排序，保证幂等可复现）。
func Plan(missing []string) []Step {
	seen := map[string]bool{}
	var tools []string
	for _, m := range missing {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		tools = append(tools, m)
	}
	sort.Strings(tools)

	steps := make([]Step, 0, len(tools))
	for _, tool := range tools {
		steps = append(steps, planOne(tool))
	}
	return steps
}

func planOne(tool string) Step {
	spec, ok := Registry[tool]
	if !ok {
		return Step{Tool: tool, Method: MethodManual,
			Note: "未登记安装方式：先确认工具官方来源，再手工安装（或在环境页添加自定义工具）"}
	}
	switch {
	case spec.APT != "":
		return Step{Tool: tool, Method: MethodAPT, Command: "apt-get install -y " + spec.APT, Note: verifyNote(spec)}
	case spec.Pipx != "":
		return Step{Tool: tool, Method: MethodPipx, Command: "pipx install " + spec.Pipx, Note: verifyNote(spec)}
	case spec.Go != "":
		return Step{Tool: tool, Method: MethodGo, Command: "go install " + spec.Go, Note: verifyNote(spec)}
	case spec.ReleaseRepo != "":
		return Step{Tool: tool, Method: MethodRelease,
			Command: fmt.Sprintf("gh release download -R %s -p '*linux_amd64*' && sha256sum -c SHA256SUMS", spec.ReleaseRepo),
			Note:    verifyNote(spec)}
	}
	return Step{Tool: tool, Method: MethodManual, Note: "该工具无自动安装方式，请手工安装"}
}

func verifyNote(spec Spec) string {
	if spec.Verify == "" {
		return ""
	}
	return "安装后自检：" + spec.Verify
}

// Fallback 返回某工具的降级链（未知工具返回 nil）。
func Fallback(tool string) []string {
	if spec, ok := Registry[tool]; ok {
		return spec.Fallback
	}
	return nil
}

// HighRisk 返回需要在安装前提示的工具（Risk=high）。
func HighRisk(missing []string) []string {
	var out []string
	for _, t := range missing {
		if s, ok := Registry[t]; ok && s.Risk == "high" {
			out = append(out, t)
		}
	}
	return out
}
