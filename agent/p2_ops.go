package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Autumn-27/artex/internal/access"
	"github.com/Autumn-27/artex/internal/creds"
	"github.com/Autumn-27/artex/internal/execx"
	"github.com/Autumn-27/artex/internal/ledger"
	"github.com/Autumn-27/artex/internal/remote"
	actool "github.com/Autumn-27/norma/tool"
)

// 本文件是 L3/L4 的「执行面」：提权 / 横移 / 战后清理从"方案"变成可执行工具，
// 执行成功后自动登记持有台账与立足点池；清理完成后把台账标 cleaned。
//
// 安全：所有命令都过 internal/execx 的策略（通用黑名单 + 清理白名单 + 超时/输出上限）。

// P2OpsTools 返回执行面工具（worker 与主控可用）。
func (t *ToolSet) P2OpsTools() []actool.CoreTool {
	if t.p2 == nil {
		return nil
	}
	return []actool.CoreTool{t.privescRun(), t.lateralRun(), t.cleanupRun()}
}

func (t *ToolSet) privescRun() actool.CoreTool {
	return t.writeExpTool("privesc_run",
		"【提权执行】在**已登记的会话**（ssh/db/webshell）上执行 privesc_plan 的一条命令——不在操作机执行。"+
			"成功后把台账记录标 verified 并写提权证据。没有会话就先 session_lease / session_register。",
		obj(map[string]any{
			"session_id": idp("会话 id（优先）；0=按 owned_id 租一个"),
			"owned_id":   idp("台账 id（没有 session_id 时用它租会话）"),
			"command":    str("要执行的提权命令（来自 privesc_plan）"),
			"timeout_s":  intp("超时秒数（默认 120）"),
		}, "command"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64  `json:"session_id"`
				OwnedID   int64  `json:"owned_id"`
				Command   string `json:"command"`
				TimeoutS  int    `json:"timeout_s"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			ses, err := t.sessionByID(ctx, a.SessionID, a.OwnedID)
			if err != nil {
				return actool.Errorf("没有可用会话（提权必须在立足点上执行）：" + err.Error()), nil
			}
			tg, _, terr := t.targetForSession(ctx, ses)
			if terr != nil {
				return actool.Errorf("会话凭据不可用：" + terr.Error()), nil
			}
			if verr := execx.Validate(a.Command, "privesc"); verr != nil {
				return actool.Errorf("策略拒绝：" + verr.Error()), nil
			}
			out, xerr := remote.Exec(ctx, tg, a.Command, time.Duration(a.TimeoutS)*time.Second)
			res := execx.Result{Command: remote.Redact(a.Command, tg.Secret), OK: xerr == nil, Output: clipText(out, 4000)}
			if xerr != nil {
				res.Err = xerr.Error()
			}
			result := map[string]any{"session": map[string]any{"id": ses.ID, "proto": ses.Proto, "owned_id": ses.OwnedID}, "result": res}
			if xerr == nil && looksRoot(out) && ses.OwnedID > 0 {
				if err := t.p2.Ledger.SetStatus(ctx, ses.OwnedID, "verified", "提权成功："+clipText(a.Command, 120)); err == nil {
					result["owned_updated"] = true
					result["privilege"] = "root/admin"
				}
			}
			t.writes.Facts++
			return jsonResult(result)
		})
}

func (t *ToolSet) lateralRun() actool.CoreTool {
	return t.writeExpTool("lateral_run",
		"【横移执行·从操作机发起】执行 lateral_plan 的一条横移命令建立新立足点；成功后自动登记持有台账 + 立足点池 + 可达网段。"+
			"这条命令是「从当前机器打到下一台」，不是目标上的命令；目标上的命令用 session_exec/privesc_run。",
		obj(map[string]any{
			"command":        str("要执行的横移命令（来自 lateral_plan，占位已替换）"),
			"target":         str("目标 host/ip"),
			"port":           intp("目标端口"),
			"access_type":    str("shell|webshell|ssh -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=3|admin|db（默认由端口推断）"),
			"privilege":      str("user|root|admin|system（默认 user）"),
			"cred_ref":       idp("使用的凭据 id（可选）"),
			"cidrs":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "该主机可达的内网段（可选，写入 routes）"},
			"via_session_id": idp("经哪个会话发起（可选；填了就在立足点上执行，用于跳板打下一台）"),
			"via_owned_id":   idp("经哪个台账主机发起（可选）"),
			"timeout_s":      intp("超时秒数（默认 120）"),
		}, "command", "target"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Command      string   `json:"command"`
				Target       string   `json:"target"`
				Port         int      `json:"port"`
				AccessType   string   `json:"access_type"`
				Privilege    string   `json:"privilege"`
				CredRef      int64    `json:"cred_ref"`
				CIDRs        []string `json:"cidrs"`
				ViaSessionID int64    `json:"via_session_id"`
				ViaOwnedID   int64    `json:"via_owned_id"`
				TimeoutS     int      `json:"timeout_s"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			to := time.Duration(a.TimeoutS) * time.Second
			var res execx.Result
			if a.ViaSessionID > 0 || a.ViaOwnedID > 0 {
				// 经立足点发起（跳板）：命令在会话/目标上执行。
				ses, serr := t.sessionByID(ctx, a.ViaSessionID, a.ViaOwnedID)
				if serr != nil {
					return actool.Errorf("经跳板执行失败（没有可用会话）：" + serr.Error()), nil
				}
				tg, _, tgErr := t.targetForSession(ctx, ses)
				if tgErr != nil {
					return actool.Errorf("跳板凭据不可用：" + tgErr.Error()), nil
				}
				if verr := execx.Validate(a.Command, "lateral"); verr != nil {
					return actool.Errorf("策略拒绝：" + verr.Error()), nil
				}
				out, xerr := remote.Exec(ctx, tg, a.Command, to)
				res = execx.Result{Command: remote.Redact(a.Command, tg.Secret), OK: xerr == nil, Output: clipText(out, 4000)}
				if xerr != nil {
					res.Err = xerr.Error()
				}
			} else {
				res = execx.Run(ctx, execx.Shell{}, a.Command, "lateral", to)
			}
			out := map[string]any{"result": res}
			if !res.OK {
				return jsonResult(out)
			}
			if a.AccessType == "" {
				a.AccessType = inferAccess(a.Port)
			}
			if a.Privilege == "" {
				a.Privilege = "user"
			}
			ownedID, created, oerr := t.p2.Ledger.Own(ctx, ledger.Owned{
				Host: a.Target, Port: a.Port, AccessType: a.AccessType, Privilege: a.Privilege,
				Depth: 1, FirstTaskID: t.taskID, Evidence: "横移成功：" + clipText(a.Command, 120),
			})
			if oerr != nil {
				out["owned_error"] = oerr.Error()
				return jsonResult(out)
			}
			out["owned_id"] = ownedID
			out["owned_created"] = created
			ses := access.Session{OwnedID: ownedID, Proto: a.AccessType, Endpoint: fmt.Sprintf("%s:%d", a.Target, a.Port),
				Host: a.Target, Port: a.Port, CredRef: a.CredRef, Notes: "lateral_run 自动登记"}
			sid, serr := t.p2.Pool.Register(ses)
			if serr == nil {
				out["session_id"] = sid
				ses.ID = sid
				_ = t.p2.PersistSession(ctx, ses)
			}
			if added, rerr := t.registerRoutes(ctx, ownedID, a.CIDRs, a.Target); rerr != nil {
				out["routes_error"] = rerr.Error()
			} else if added > 0 {
				out["routes_added"] = added
			}
			return jsonResult(out)
		})
}

func inferAccess(port int) string {
	switch port {
	case 22:
		return "ssh"
	case 445, 5985, 5986, 3389:
		return "admin"
	case 3306, 5432, 1433, 6379, 27017:
		return "db"
	}
	return "shell"
}

func (t *ToolSet) cleanupRun() actool.CoreTool {
	return t.writeExpTool("cleanup_run",
		"【战后清理】在**已登记的会话**上执行清理命令（删 webshell/临时文件/后门账号/计划任务），"+
			"全部成功后台账自动标 cleaned、会话标 down。清理命令走白名单（系统根一律拒绝）。",
		obj(map[string]any{
			"session_id": idp("会话 id（优先）"),
			"owned_id":   idp("台账 id（没有 session_id 时用它租会话）"),
			"commands":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "清理命令数组"},
			"detail":     str("清理项说明（写进台账）"),
			"timeout_s":  intp("单条超时秒数（默认 60）"),
		}, "commands"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64    `json:"session_id"`
				OwnedID   int64    `json:"owned_id"`
				Commands  []string `json:"commands"`
				Detail    string   `json:"detail"`
				TimeoutS  int      `json:"timeout_s"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if len(a.Commands) == 0 {
				return actool.Errorf("commands 不能为空"), nil
			}
			if len(a.Commands) > 20 {
				return actool.Errorf("一次最多 20 条清理命令"), nil
			}
			ses, err := t.sessionByID(ctx, a.SessionID, a.OwnedID)
			if err != nil {
				return actool.Errorf("没有可用会话（清理必须在立足点上执行）：" + err.Error()), nil
			}
			tg, _, terr := t.targetForSession(ctx, ses)
			if terr != nil {
				return actool.Errorf("会话凭据不可用：" + terr.Error()), nil
			}
			to := time.Duration(a.TimeoutS) * time.Second
			results := make([]execx.Result, 0, len(a.Commands))
			okCount := 0
			for _, cmd := range a.Commands {
				if verr := execx.Validate(cmd, "cleanup"); verr != nil {
					results = append(results, execx.Result{Command: cmd, Err: "策略拒绝：" + verr.Error()})
					continue
				}
				out, xerr := remote.Exec(ctx, tg, cmd, to)
				r := execx.Result{Command: remote.Redact(cmd, tg.Secret), OK: xerr == nil, Output: clipText(out, 2000)}
				if xerr != nil {
					r.Err = xerr.Error()
				}
				results = append(results, r)
				if r.OK {
					okCount++
				}
			}
			out := map[string]any{"results": results, "ok": okCount, "total": len(a.Commands),
				"session": map[string]any{"id": ses.ID, "proto": ses.Proto, "owned_id": ses.OwnedID}}
			if ses.OwnedID > 0 && okCount == len(a.Commands) {
				detail := a.Detail
				if strings.TrimSpace(detail) == "" {
					detail = fmt.Sprintf("执行 %d 条清理命令", okCount)
				}
				if err := t.p2.Ledger.Cleanup(ctx, ses.OwnedID, detail); err == nil {
					out["cleaned"] = true
					t.p2.Pool.Mark(ses.ID, "down")
				}
			}
			return jsonResult(out)
		})
}

// PersistSession 把会话写入 access_refs（owned_id 必须 >0；重启后可恢复）。
func (p *P2) PersistSession(ctx context.Context, s access.Session) error {
	if p == nil || p.DB == nil || s.OwnedID <= 0 {
		return nil
	}
	conn, _ := json.Marshal(map[string]any{
		"endpoint": s.Endpoint, "host": s.Host, "port": s.Port,
		"user": s.User, "cmd_param": s.CmdParam,
	})
	_, err := p.DB.ExecContext(ctx,
		`INSERT INTO access_refs (owned_id, proto, conn, cred_ref, status)
		 VALUES ($1, $2, $3::jsonb, NULLIF($4,0), COALESCE(NULLIF($5,''),'up'))
		 ON CONFLICT DO NOTHING`,
		s.OwnedID, s.Proto, string(conn), s.CredRef, s.Status)
	return err
}

// LoadSessions 从 access_refs 恢复会话到内存池（服务启动时调用）。
func (p *P2) LoadSessions(ctx context.Context) (int, error) {
	if p == nil || p.DB == nil || p.Pool == nil {
		return 0, nil
	}
	rows, err := p.DB.QueryContext(ctx,
		`SELECT id, owned_id, proto, conn, COALESCE(cred_ref,0), status
		 FROM access_refs WHERE status = 'up' ORDER BY id LIMIT 500`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id, ownedID, credRef int64
		var proto, status string
		var raw []byte
		if rows.Scan(&id, &ownedID, &proto, &raw, &credRef, &status) != nil {
			continue
		}
		var conn struct {
			Endpoint string `json:"endpoint"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			User     string `json:"user"`
			CmdParam string `json:"cmd_param"`
		}
		_ = json.Unmarshal(raw, &conn)
		p.Pool.Restore(access.Session{
			ID: id, OwnedID: ownedID, Proto: proto, Endpoint: conn.Endpoint,
			Host: conn.Host, Port: conn.Port, User: conn.User, CmdParam: conn.CmdParam,
			CredRef: credRef, Status: status,
		})
		n++
	}
	return n, rows.Err()
}

// targetForSession 把会话 + 凭据组装成远程执行目标（秘密只留内存）。
func (t *ToolSet) targetForSession(ctx context.Context, s access.Session) (remote.Target, string, error) {
	tg := remote.Target{Proto: s.Proto, Host: s.Host, Port: s.Port, Endpoint: s.Endpoint,
		User: s.User, CmdParam: s.CmdParam}
	if tg.Host == "" && tg.Endpoint != "" {
		tg.Host = s.Endpoint
	}
	if tg.Host == "" && tg.Endpoint == "" && s.OwnedID > 0 {
		if o, err := t.p2.Ledger.Get(ctx, s.OwnedID); err == nil {
			tg.Host = firstNonEmpty(o.Host, o.IP)
			if tg.Port == 0 {
				tg.Port = o.Port
			}
		}
	}
	secret := ""
	if s.CredRef > 0 {
		e, pt, err := t.p2.Creds.Get(ctx, s.CredRef)
		if err != nil {
			return tg, "", err
		}
		secret = pt
		if tg.User == "" {
			tg.User = e.Username
		}
	}
	tg.Secret = secret
	return tg, secret, nil
}

// sessionByID 取会话；没有 id 时按 owned_id 租一个。
func (t *ToolSet) sessionByID(ctx context.Context, sessionID, ownedID int64) (access.Session, error) {
	if sessionID > 0 {
		if s, ok := t.p2.Pool.Get(sessionID); ok {
			if s.Worker != "" && s.Worker != t.worker {
				if leased, err := t.p2.Pool.Lease(t.worker, s.OwnedID); err == nil {
					return *leased, nil
				}
			}
			return *s, nil
		}
	}
	leased, err := t.p2.Pool.Lease(t.worker, ownedID)
	if err != nil {
		return access.Session{}, err
	}
	return *leased, nil
}

func (t *ToolSet) sessionExec() actool.CoreTool {
	return t.writeExpTool("session_exec",
		"【立足点执行】在**已登记的会话**上执行命令（ssh/db/webshell），不是在操作机上跑。跨 worker 共享："+
			"别人登记的会话也能租用。命令会过安全策略；秘密只走环境变量，不回显。",
		obj(map[string]any{
			"session_id": idp("会话 id（session_list 返回；0=按 owned_id 租一个）"),
			"owned_id":   idp("台账 id（没有 session_id 时用它租会话）"),
			"command":    str("要执行的命令"),
			"kind":       str("lateral|privesc|cleanup（决定策略，默认 lateral）"),
			"timeout_s":  intp("超时秒数（默认 120）"),
		}, "command"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				SessionID int64  `json:"session_id"`
				OwnedID   int64  `json:"owned_id"`
				Command   string `json:"command"`
				Kind      string `json:"kind"`
				TimeoutS  int    `json:"timeout_s"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			if a.Kind == "" {
				a.Kind = "lateral"
			}
			ses, err := t.sessionByID(ctx, a.SessionID, a.OwnedID)
			if err != nil {
				return actool.Errorf("没有可用会话：" + err.Error()), nil
			}
			tg, _, terr := t.targetForSession(ctx, ses)
			if terr != nil {
				return actool.Errorf("会话凭据不可用：" + terr.Error()), nil
			}
			if verr := execx.Validate(a.Command, a.Kind); verr != nil {
				return actool.Errorf("策略拒绝：" + verr.Error()), nil
			}
			out, xerr := remote.Exec(ctx, tg, a.Command, time.Duration(a.TimeoutS)*time.Second)
			res := execx.Result{Command: remote.Redact(a.Command, tg.Secret), OK: xerr == nil,
				Output: clipText(out, 4000)}
			if xerr != nil {
				res.Err = xerr.Error()
			}
			return jsonResult(map[string]any{"session": map[string]any{"id": ses.ID, "proto": ses.Proto,
				"owned_id": ses.OwnedID, "endpoint": ses.Endpoint}, "result": res})
		})
}

func looksRoot(out string) bool {
	l := strings.ToLower(out)
	for _, m := range []string{"uid=0(root)", "uid=0", "root@", "nt authority", "whoami: root", "euid=0"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// registerRoutes 把横移主机可达的网段写进 routes（幂等）。
func (t *ToolSet) registerRoutes(ctx context.Context, ownedID int64, cidrs []string, host string) (int, error) {
	if t.p2 == nil || t.p2.DB == nil || len(cidrs) == 0 {
		return 0, nil
	}
	added := 0
	for _, raw := range cidrs {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(c); err != nil {
			continue
		}
		_, err := t.p2.DB.ExecContext(ctx,
			`INSERT INTO routes (task_id, cidr, via_owned, proxy_endpoint, hops, status)
			 VALUES (NULLIF($1,0), $2::cidr, NULLIF($3,0), $4, 1, 'up')
			 ON CONFLICT (COALESCE(task_id,0), cidr, COALESCE(via_owned,0)) DO NOTHING`,
			t.taskID, c, ownedID, "socks5://"+host+":1080")
		if err == nil {
			added++
		}
	}
	return added, nil
}

// credReuseRun 把「凭据复用计划」真正执行掉：解密 → 逐服务尝试 → 标记使用/验证。
func (t *ToolSet) credReuseRun() actool.CoreTool {
	return writeTool("cred_reuse_run",
		"【凭据复用执行】取一条凭据，按已扫描端口尝试 ssh/mysql/postgres/redis 登录（秘密只走环境变量，不回显）。"+
			"每次尝试都记 use_count；成功即标记 verified。smb/winrm 等需要 nxc 的协议会跳过并说明。",
		obj(map[string]any{
			"cred_id":   idp("凭据 id"),
			"host":      str("目标 host/ip"),
			"ports":     map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "开放端口（空=按常见服务依次试）"},
			"timeout_s": intp("单次超时秒数（默认 20）"),
		}, "cred_id", "host"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				CredID   int64  `json:"cred_id"`
				Host     string `json:"host"`
				Ports    []int  `json:"ports"`
				TimeoutS int    `json:"timeout_s"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			e, secret, err := t.p2.Creds.Get(ctx, a.CredID)
			if err != nil {
				return actool.Errorf("凭据不存在：" + err.Error()), nil
			}
			to := time.Duration(a.TimeoutS) * time.Second
			if to <= 0 {
				to = 20 * time.Second
			}
			plan := creds.PlanReuse(creds.Target{Host: a.Host}, e, a.Ports)
			results := make([]map[string]any, 0, len(plan))
			verified := false
			for _, at := range plan {
				var tgProto, cmd string
				switch at.Proto {
				case "ssh":
					tgProto, cmd = "ssh", "id"
				case "mysql":
					tgProto, cmd = "mysql", "SELECT 1"
				case "postgres":
					tgProto, cmd = "postgres", "SELECT 1"
				case "redis":
					tgProto, cmd = "redis", "PING"
				default:
					results = append(results, map[string]any{"proto": at.Proto, "port": at.Target.Port,
						"skipped": "该协议需要 nxc/交互认证，暂不自动复用（用 lateral_run 手动）"})
					continue
				}
				_ = t.p2.Creds.MarkUsed(ctx, a.CredID, fmt.Sprintf("%s:%d", a.Host, at.Target.Port))
				tg := remote.Target{Proto: tgProto, Host: a.Host, Port: at.Target.Port, User: e.Username, Secret: secret}
				out, xerr := remote.Exec(ctx, tg, cmd, to)
				ok := xerr == nil
				row := map[string]any{"proto": at.Proto, "port": at.Target.Port, "ok": ok,
					"output": clipText(out, 600), "cmd": remote.Redact(cmd, secret)}
				if xerr != nil {
					row["err"] = xerr.Error()
				}
				results = append(results, row)
				if ok {
					verified = true
					break
				}
			}
			// 后台/HTTP Basic 复用（只验证认证是否通过，不读数据）。
			for _, at := range creds.PlanWebReuse(a.Host, a.Ports) {
				_ = t.p2.Creds.MarkUsed(ctx, a.CredID, fmt.Sprintf("%s:%d", a.Host, at.Target.Port))
				tg := remote.Target{Proto: "httpbasic", Host: a.Host, Port: at.Target.Port, User: e.Username, Secret: secret}
				out, xerr := remote.Exec(ctx, tg, "/", to)
				code := strings.TrimSpace(out)
				ok := xerr == nil && strings.HasPrefix(code, "2")
				results = append(results, map[string]any{"proto": "httpbasic", "port": at.Target.Port, "ok": ok, "http_code": code})
				if ok {
					verified = true
					break
				}
			}
			if verified {
				_ = t.p2.Creds.SetVerified(ctx, a.CredID, true)
			}
			return jsonResult(map[string]any{"cred_id": a.CredID, "host": a.Host, "verified": verified, "attempts": results})
		})
}

// routeResolve 读取 routes 表：目标网段能经哪台跳板/代理到达。
func (t *ToolSet) routeResolve() actool.CoreTool {
	return readTool("route_resolve",
		"【路由解析】查 routes 表：给定目标 IP/CIDR，返回可经的跳板（via_owned）、代理端点与跳数。"+
			"lateral_run 登记的可达网段在这里被实际读取使用。",
		obj(map[string]any{"target": str("目标 IP 或 CIDR")}, "target"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Target string `json:"target"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败：" + err.Error()), nil
			}
			tg := strings.TrimSpace(a.Target)
			if tg == "" {
				return actool.Errorf("target 不能为空"), nil
			}
			if net.ParseIP(tg) == nil {
				if _, _, err := net.ParseCIDR(tg); err != nil {
					return actool.Errorf("target 不是合法 IP/CIDR"), nil
				}
			}
			if t.p2 == nil || t.p2.DB == nil {
				return actool.Errorf("路由解析需要 PostgreSQL"), nil
			}
			rows, err := t.p2.DB.QueryContext(ctx,
				`SELECT cidr::text, COALESCE(via_owned,0), proxy_endpoint, hops, status
				 FROM routes WHERE $1::inet <<= cidr ORDER BY hops ASC LIMIT 5`, tg)
			if err != nil {
				return actool.Errorf("查询失败：" + err.Error()), nil
			}
			defer rows.Close()
			out := []map[string]any{}
			for rows.Next() {
				var cidr, proxy, status string
				var via int64
				var hops int
				if rows.Scan(&cidr, &via, &proxy, &hops, &status) == nil {
					out = append(out, map[string]any{"cidr": cidr, "via_owned": via,
						"proxy_endpoint": proxy, "hops": hops, "status": status})
				}
			}
			return jsonResult(map[string]any{"target": tg, "routes": out})
		})
}
