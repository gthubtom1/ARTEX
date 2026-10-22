package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/adapter"
	"github.com/Autumn-27/artex/internal/egress"
	"github.com/Autumn-27/artex/internal/envfilter"
	actool "github.com/Autumn-27/norma/tool"
)

// 本文件把 nmap / httpx / nuclei 注册成 typed host tools：
// agent 不再拼 shell 字符串，输出直接是结构化 JSON（token −50%，且不会拼错参数）。
// 原始输出同时落盘（证据），返回 raw_ref 供报告引用。

var safeTargetRe = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]\-*,]+$`)

// AdapterTools 返回三个结构化扫描工具。
func (t *ToolSet) AdapterTools() []actool.CoreTool {
	return []actool.CoreTool{t.nmapScan(), t.httpxProbe(), t.nucleiScan()}
}

func (t *ToolSet) nmapScan() actool.CoreTool {
	return readTool("nmap_scan",
		"【结构化扫描】跑 nmap 并把 XML 解析成 JSON（端口/服务/版本），不再返回整屏文本。"+
			"默认 -sV -Pn --version-light（只做服务识别，不跑脚本）。目标支持域名/IP/CIDR。",
		obj(map[string]any{
			"target":  str("目标：域名 / IP / CIDR（逗号分隔）"),
			"ports":   str("端口范围，如 22,80,443 或 1-1000（可选；默认只扫常见端口）"),
			"timeout": intp("超时秒数（默认 300）"),
		}, "target"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Target  string `json:"target"`
				Ports   string `json:"ports"`
				Timeout int    `json:"timeout"`
			}
			_ = json.Unmarshal(in, &a)
			if err := checkTarget(a.Target); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			args := []string{"-sV", "-Pn", "--version-light", "-oX", "-", a.Target}
			if strings.TrimSpace(a.Ports) != "" {
				if !regexp.MustCompile(`^[0-9,\-]+$`).MatchString(a.Ports) {
					return actool.Errorf("ports 只接受数字、逗号和连字符（如 22,80,443 或 1-1000）"), nil
				}
				args = append([]string{"-p", a.Ports}, args...)
			}
			return t.runAdapter(ctx, "nmap", args, a.Timeout, adapter.NmapXML())
		})
}

func (t *ToolSet) httpxProbe() actool.CoreTool {
	return readTool("httpx_probe",
		"【结构化探测】跑 httpx 并把 JSONL 解析成 JSON（状态码/标题/服务器/技术栈/CDN）。"+
			"输入一个域名或 IP，返回存活 Web 面；比 curl 逐个人工试快得多。",
		obj(map[string]any{
			"target":  str("目标：域名 / IP / URL（可选带端口）"),
			"timeout": intp("超时秒数（默认 120）"),
		}, "target"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Target  string `json:"target"`
				Timeout int    `json:"timeout"`
			}
			_ = json.Unmarshal(in, &a)
			if err := checkTarget(a.Target); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			args := []string{"-json", "-silent", "-no-color", "-u", a.Target}
			return t.runAdapter(ctx, "httpx", args, a.Timeout, adapter.HttpxJSONL())
		})
}

func (t *ToolSet) nucleiScan() actool.CoreTool {
	return readTool("nuclei_scan",
		"【结构化扫描】跑 nuclei 并把 JSONL 解析成 JSON（模板/严重性/命中 URL）。"+
			"用于已知 CVE / 错配 / 暴露面模板化验证；命中只是线索，报 finding 前仍要过 verify_claim。",
		obj(map[string]any{
			"target":   str("目标 URL 或域名"),
			"severity": str("严重性过滤：critical,high,medium（可选）"),
			"tags":     str("模板标签过滤，如 cve,exposure（可选）"),
			"timeout":  intp("超时秒数（默认 600）"),
		}, "target"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Target   string `json:"target"`
				Severity string `json:"severity"`
				Tags     string `json:"tags"`
				Timeout  int    `json:"timeout"`
			}
			_ = json.Unmarshal(in, &a)
			if err := checkTarget(a.Target); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			args := []string{"-jsonl", "-silent", "-no-color", "-u", a.Target}
			if s := strings.TrimSpace(a.Severity); s != "" {
				if !regexp.MustCompile(`^[a-z,]+$`).MatchString(s) {
					return actool.Errorf("severity 只接受小写英文与逗号"), nil
				}
				args = append(args, "-severity", s)
			}
			if tg := strings.TrimSpace(a.Tags); tg != "" {
				if !regexp.MustCompile(`^[a-z0-9,\-_]+$`).MatchString(tg) {
					return actool.Errorf("tags 只接受小写英文、数字、逗号、连字符"), nil
				}
				args = append(args, "-tags", tg)
			}
			return t.runAdapter(ctx, "nuclei", args, a.Timeout, adapter.NucleiJSONL())
		})
}

// runAdapter 执行命令 → 落盘原始输出 → 结构化解析 → 返回 JSON+摘要。
func (t *ToolSet) runAdapter(ctx context.Context, tool string, args []string, timeoutSec int, parser adapter.Parser) (actool.Result, error) {
	bin, err := exec.LookPath(tool)
	if err != nil {
		return actool.Errorf(fmt.Sprintf("%s 未安装：先 provision 安装（apt/pipx/go/release），或用 Bash 走等价的降级做法（curl/nc）。原始参数：%s %s",
			tool, tool, strings.Join(args, " "))), nil
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	// 出口闸：这些是真正对外发流量的扫描工具，必须与 execx/remote 一样 fail-closed 并走代理。
	if err := egress.Guard(ctx); err != nil {
		return actool.Errorf("出口未就绪，拒绝执行扫描：" + err.Error()), nil
	}
	c, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(c, bin, args...)
	// P8-7：剥掉凭据主密钥/数据库口令等，Bash 子进程不该看到它们。
	cmd.Env = envfilter.Strip(os.Environ())
	if len(egress.ProxyEnv) > 0 {
		cmd.Env = append(cmd.Env, egress.ProxyEnv...)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := stdout.String()
	errText := stderr.String()
	if err != nil && out == "" {
		return actool.Errorf(fmt.Sprintf("%s 执行失败：%v；stderr：%s", tool, err, clipText(errText, 1200))), nil
	}

	rawRef := t.writeRaw(tool, out)
	res, perr := parser.Parse(out, errText)
	res.Raw_Ref = rawRef
	if perr != nil {
		res.OK = false
		res.Err = perr.Error()
	}
	summary := adapter.Summarize(res)
	payload, _ := json.Marshal(map[string]any{
		"summary": summary,
		"result":  res,
		"raw_ref": rawRef,
	})
	return actool.Text(string(payload)), nil
}

// writeRaw 把原始输出落盘（证据），失败不影响主流程。
func (t *ToolSet) writeRaw(tool, out string) string {
	dir := t.workDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "artex-adapters")
	} else {
		dir = filepath.Join(dir, "adapters")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	name := fmt.Sprintf("%s-%d-%d.txt", tool, t.taskID, time.Now().UnixNano())
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		return ""
	}
	return p
}

func checkTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target 不能为空")
	}
	if len(target) > 512 {
		return fmt.Errorf("target 过长")
	}
	// 允许逗号分隔多目标：先按逗号切开逐个校验。
	for _, part := range strings.Split(target, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !safeTargetRe.MatchString(part) {
			return fmt.Errorf("target 含非法字符：%q（只接受域名/IP/CIDR/URL）", part)
		}
	}
	return nil
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
