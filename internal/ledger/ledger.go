// Package ledger 是「长期持有台账」：打下来的东西登记在 assets_owned，
// 定期复查（还活着吗 / 权限变了吗），失效自动标 stale/lost，战后标 cleaned。
//
// 设计纪律（docs/最终方案-v1.md §3.2）：
//   - assets_owned 是**全局**的（跨任务同一条），带 first_task_id；
//   - 复查写入 asset_checks（带业务幂等键，同一轮只记一次）；
//   - 台账可导出 JSON，给报告/前端直接用。
package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Owned 是一条持有记录。
type Owned struct {
	ID             int64     `json:"id"`
	AssetKey       string    `json:"asset_key"`
	AssetID        int64     `json:"asset_id,omitempty"`
	Host           string    `json:"host,omitempty"`
	IP             string    `json:"ip,omitempty"`
	Port           int       `json:"port,omitempty"`
	AccessType     string    `json:"access_type"` // shell|webshell|ssh|vpn|jump|admin|db
	Privilege      string    `json:"privilege"`   // user|root|admin|system
	Depth          int       `json:"depth"`
	ViaAsset       int64     `json:"via_asset,omitempty"`
	Status         string    `json:"status"` // owned|verified|stale|lost|cleaned
	FirstTaskID    int64     `json:"first_task_id,omitempty"`
	FirstOwnedAt   time.Time `json:"first_owned_at"`
	LastVerifiedAt time.Time `json:"last_verified_at,omitempty"`
	NextCheckAt    time.Time `json:"next_check_at,omitempty"`
	Evidence       string    `json:"evidence,omitempty"`
	Notes          string    `json:"notes,omitempty"`
}

// Check 是一次复查结果。
type Check struct {
	OwnedID   int64     `json:"owned_id"`
	AssetID   int64     `json:"asset_id,omitempty"`
	Kind      string    `json:"kind"` // http|ssh|port|login|webshell
	OK        bool      `json:"ok"`
	LatencyMS int       `json:"latency_ms,omitempty"`
	Changed   bool      `json:"changed"`
	Detail    string    `json:"detail,omitempty"`
	TS        time.Time `json:"ts"`
}

// Store 是台账存储；db 为 nil 时退化为内存实现。
type Store struct {
	db         dbiface.DB
	recheck    time.Duration // 下次复查间隔
	staleAfter time.Duration // 多久没复查算 stale

	mu   sync.Mutex
	mem  map[string]*Owned
	next int64
}

// NewStore 创建台账。recheckHours/staleDays 来自 settings。
func NewStore(db dbiface.DB, recheckHours, staleDays int) *Store {
	if recheckHours <= 0 {
		recheckHours = 24
	}
	if staleDays <= 0 {
		staleDays = 7
	}
	return &Store{
		db:         db,
		recheck:    time.Duration(recheckHours) * time.Hour,
		staleAfter: time.Duration(staleDays) * 24 * time.Hour,
		mem:        map[string]*Owned{},
	}
}

// AssetKey 生成全局稳定键：优先 host（域名），否则 ip:port。
func AssetKey(host, ip string, port int, accessType string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	ip = strings.TrimSpace(ip)
	switch {
	case host != "" && port > 0:
		return fmt.Sprintf("%s:%d/%s", host, port, accessType)
	case host != "":
		return host + "/" + accessType
	case ip != "" && port > 0:
		return fmt.Sprintf("%s:%d/%s", ip, port, accessType)
	case ip != "":
		return ip + "/" + accessType
	}
	return "unknown/" + accessType
}

// Own 登记（或更新）一条持有记录。同 asset_key 返回已有 id，created=false。
func (s *Store) Own(ctx context.Context, o Owned) (id int64, created bool, err error) {
	if o.AccessType == "" {
		o.AccessType = "shell"
	}
	if o.Status == "" {
		o.Status = "owned"
	}
	if o.AssetKey == "" {
		o.AssetKey = AssetKey(o.Host, o.IP, o.Port, o.AccessType)
	}
	if o.AssetKey == "unknown/"+o.AccessType {
		return 0, false, fmt.Errorf("ledger: host/ip 至少给一个")
	}
	now := time.Now()
	if o.FirstOwnedAt.IsZero() {
		o.FirstOwnedAt = now
	}
	if o.NextCheckAt.IsZero() {
		o.NextCheckAt = now.Add(s.recheck)
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if cur, ok := s.mem[o.AssetKey]; ok {
			cur.AssetID = firstNonZero(o.AssetID, cur.AssetID)
			cur.Evidence = firstNonEmpty(o.Evidence, cur.Evidence)
			cur.Notes = firstNonEmpty(o.Notes, cur.Notes)
			cur.Status = o.Status
			return cur.ID, false, nil
		}
		s.next++
		o.ID = s.next
		cp := o
		s.mem[o.AssetKey] = &cp
		return o.ID, true, nil
	}
	var existing int64
	err = s.db.QueryRowContext(ctx, `SELECT id FROM assets_owned WHERE asset_key = $1`, o.AssetKey).Scan(&existing)
	if err == nil {
		_, uerr := s.db.ExecContext(ctx,
			`UPDATE assets_owned SET status = $2,
			 asset_id = COALESCE(NULLIF($3,0), asset_id),
			 evidence = CASE WHEN $4 = '' THEN evidence ELSE $4 END,
			 notes = CASE WHEN $5 = '' THEN notes ELSE $5 END,
			 next_check_at = COALESCE(next_check_at, $6)
			 WHERE id = $1`,
			existing, o.Status, o.AssetID, o.Evidence, o.Notes, o.NextCheckAt)
		if uerr != nil {
			return 0, false, fmt.Errorf("ledger: update: %w", uerr)
		}
		return existing, false, nil
	}
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO assets_owned (asset_key, asset_id, host, ip, port, access_type, privilege, depth, via_asset,
		 status, first_task_id, first_owned_at, next_check_at, evidence, notes)
		 VALUES ($1,NULLIF($2,0),$3,$4,NULLIF($5,0),$6,$7,$8,NULLIF($9,0),$10,NULLIF($11,0),$12,$13,$14,$15)
		 RETURNING id`,
		o.AssetKey, o.AssetID, o.Host, o.IP, o.Port, o.AccessType, o.Privilege, o.Depth, o.ViaAsset,
		o.Status, o.FirstTaskID, o.FirstOwnedAt, o.NextCheckAt, o.Evidence, o.Notes).Scan(&id)
	if err != nil {
		return 0, false, fmt.Errorf("ledger: insert: %w", err)
	}
	return id, true, nil
}

// Get 取一条。
func (s *Store) Get(ctx context.Context, id int64) (Owned, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, o := range s.mem {
			if o.ID == id {
				return *o, nil
			}
		}
		return Owned{}, fmt.Errorf("ledger: id %d 不存在", id)
	}
	var o Owned
	err := s.db.QueryRowContext(ctx,
		`SELECT id, asset_key, COALESCE(asset_id,0), host, ip, COALESCE(port,0), access_type, privilege, depth,
		 COALESCE(via_asset,0), status, COALESCE(first_task_id,0), first_owned_at,
		 COALESCE(last_verified_at,'0001-01-01'::timestamptz), COALESCE(next_check_at,'0001-01-01'::timestamptz), evidence, notes
		 FROM assets_owned WHERE id = $1`, id).
		Scan(&o.ID, &o.AssetKey, &o.AssetID, &o.Host, &o.IP, &o.Port, &o.AccessType, &o.Privilege, &o.Depth,
			&o.ViaAsset, &o.Status, &o.FirstTaskID, &o.FirstOwnedAt, &o.LastVerifiedAt, &o.NextCheckAt, &o.Evidence, &o.Notes)
	if err != nil {
		return Owned{}, fmt.Errorf("ledger: get %d: %w", id, err)
	}
	return o, nil
}

// List 列出持有记录（status 为空 → 全部；cleaned/lost 默认也返回，由调用方过滤）。
func (s *Store) List(ctx context.Context, status string, limit int) ([]Owned, error) {
	if limit <= 0 {
		limit = 200
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := make([]Owned, 0, len(s.mem))
		for _, o := range s.mem {
			if status != "" && o.Status != status {
				continue
			}
			out = append(out, *o)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		if len(out) > limit {
			out = out[:limit]
		}
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, asset_key, COALESCE(asset_id,0), host, ip, COALESCE(port,0), access_type, privilege, depth,
		 COALESCE(via_asset,0), status, COALESCE(first_task_id,0), first_owned_at,
		 COALESCE(last_verified_at,'0001-01-01'::timestamptz), COALESCE(next_check_at,'0001-01-01'::timestamptz), evidence, notes
		 FROM assets_owned WHERE ($1 = '' OR status = $1) ORDER BY id DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: list: %w", err)
	}
	defer rows.Close()
	var out []Owned
	for rows.Next() {
		var o Owned
		if err := rows.Scan(&o.ID, &o.AssetKey, &o.AssetID, &o.Host, &o.IP, &o.Port, &o.AccessType, &o.Privilege,
			&o.Depth, &o.ViaAsset, &o.Status, &o.FirstTaskID, &o.FirstOwnedAt, &o.LastVerifiedAt, &o.NextCheckAt,
			&o.Evidence, &o.Notes); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Due 返回需要复查的记录（next_check_at <= now 且未 cleaned/lost）。
func (s *Store) Due(ctx context.Context, now time.Time, limit int) ([]Owned, error) {
	all, err := s.List(ctx, "", limit*3)
	if err != nil {
		return nil, err
	}
	var out []Owned
	for _, o := range all {
		if o.Status == "cleaned" || o.Status == "lost" {
			continue
		}
		if o.NextCheckAt.IsZero() || !o.NextCheckAt.After(now) {
			out = append(out, o)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// RecordCheck 记录一次复查：ok → verified；连续失败或超过 staleAfter → stale。
func (s *Store) RecordCheck(ctx context.Context, c Check) error {
	if c.Kind == "" {
		c.Kind = "port"
	}
	if c.TS.IsZero() {
		c.TS = time.Now()
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, o := range s.mem {
			if o.ID != c.OwnedID {
				continue
			}
			o.LastVerifiedAt = c.TS
			next := s.recheck
			if !c.OK {
				next = 30 * time.Minute // 失败尽快重试，而不是等满一个周期
			}
			o.NextCheckAt = c.TS.Add(next)
			if c.OK {
				o.Status = "verified"
			} else if c.TS.Sub(o.LastVerifiedAt) > s.staleAfter {
				o.Status = "stale"
			} else {
				o.Status = "stale"
			}
			return nil
		}
		return fmt.Errorf("ledger: owned %d 不存在", c.OwnedID)
	}
	status := "stale"
	if c.OK {
		status = "verified"
	}
	checkKey := c.TS.UTC().Format("2006-01-02T15") // 同一小时同一类型只记一条
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO asset_checks (owned_id, asset_id, ts, kind, ok, latency_ms, changed, check_key, detail)
		 VALUES ($1,NULLIF($2,0),$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (owned_id, kind, check_key) WHERE check_key <> '' DO UPDATE SET ok = EXCLUDED.ok,
		 latency_ms = EXCLUDED.latency_ms, changed = EXCLUDED.changed, detail = EXCLUDED.detail`,
		c.OwnedID, c.AssetID, c.TS, c.Kind, c.OK, c.LatencyMS, c.Changed, checkKey, c.Detail)
	if err != nil {
		return fmt.Errorf("ledger: record check: %w", err)
	}
	next := s.recheck
	if !c.OK {
		next = 30 * time.Minute
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE assets_owned SET status = $2, last_verified_at = CASE WHEN $3 THEN $4 ELSE last_verified_at END,
		 next_check_at = $4 + $5::interval WHERE id = $1`,
		c.OwnedID, status, c.OK, c.TS, fmt.Sprintf("%d seconds", int(next.Seconds())))
	return err
}

// SetStatus 直接改状态（lost / cleaned / owned …）。
func (s *Store) SetStatus(ctx context.Context, id int64, status, notes string) error {
	switch status {
	case "owned", "verified", "stale", "lost", "cleaned":
	default:
		return fmt.Errorf("ledger: 非法状态 %q", status)
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, o := range s.mem {
			if o.ID == id {
				o.Status = status
				if notes != "" {
					o.Notes = notes
				}
				if status == "cleaned" {
					o.NextCheckAt = time.Time{}
				}
				return nil
			}
		}
		return fmt.Errorf("ledger: id %d 不存在", id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE assets_owned SET status = $2,
		 notes = CASE WHEN $3 = '' THEN notes ELSE notes || E'\n' || $3 END,
		 next_check_at = CASE WHEN $2 = 'cleaned' THEN NULL ELSE next_check_at END
		 WHERE id = $1`, id, status, notes)
	return err
}

// Cleanup 战后清理：把记录标 cleaned，并留下清理项说明。
func (s *Store) Cleanup(ctx context.Context, id int64, detail string) error {
	if strings.TrimSpace(detail) == "" {
		detail = "清理项未记录"
	}
	return s.SetStatus(ctx, id, "cleaned", "清理："+detail)
}

// Stats 返回各状态计数。
func (s *Store) Stats(ctx context.Context) (map[string]int, error) {
	all, err := s.List(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, o := range all {
		out[o.Status]++
	}
	out["total"] = len(all)
	return out, nil
}

// ExportJSON 导出整本台账（报告/前端用）。
func (s *Store) ExportJSON(ctx context.Context) ([]byte, error) {
	all, err := s.List(ctx, "", 0)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(map[string]any{
		"exported_at": time.Now().Format(time.RFC3339),
		"count":       len(all),
		"assets":      all,
	}, "", "  ")
}

// CheckSpec 是一条待执行的复查动作。
type CheckSpec struct {
	OwnedID int64  `json:"owned_id"`
	Kind    string `json:"kind"`
	Command string `json:"command"`
	Expect  string `json:"expect"`
}

// PlanChecks 按持有类型生成复查动作（只读、低成本）。
func PlanChecks(owned []Owned) []CheckSpec {
	var out []CheckSpec
	for _, o := range owned {
		addr := firstNonEmpty(o.Host, o.IP)
		port := o.Port
		if !SafeHost(addr) {
			out = append(out, CheckSpec{OwnedID: o.ID, Kind: "manual",
				Command: "", Expect: "人工复查（主机名含非法字符，命令未生成）"})
			continue
		}
		switch o.AccessType {
		case "ssh":
			if port == 0 {
				port = 22
			}
			out = append(out, netCheck(o.ID, "ssh", addr, port))
		case "webshell", "admin":
			url := addr
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				url = "http://" + url
			}
			// 端口要带上（否则默认 80/443，复查会查错服务）。
			if port > 0 && port != 80 && port != 443 && !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://"), ":") {
				url = strings.TrimRight(url, "/") + fmt.Sprintf(":%d/", port)
			}
			if !SafeURL(url) {
				out = append(out, CheckSpec{OwnedID: o.ID, Kind: "manual", Command: "",
					Expect: "人工复查（URL 含非法字符）"})
				continue
			}
			// http_code=000 表示连不上；-w 只输出状态码，解析在 Runner 里做。
			out = append(out, CheckSpec{OwnedID: o.ID, Kind: "http",
				Command: fmt.Sprintf("curl -s -o /dev/null -w '%%{http_code}' -m 10 %s", url),
				Expect:  "2xx/3xx"})
		case "db":
			if port == 0 {
				port = 3306
			}
			out = append(out, netCheck(o.ID, "port", addr, port))
		default: // shell / vpn / jump
			if port == 0 {
				out = append(out, CheckSpec{OwnedID: o.ID, Kind: "manual", Command: "",
					Expect: "人工复查（台账没记端口）"})
				continue
			}
			out = append(out, netCheck(o.ID, "port", addr, port))
		}
	}
	return out
}

func netCheck(ownedID int64, kind, addr string, port int) CheckSpec {
	if port <= 0 || port > 65535 {
		return CheckSpec{OwnedID: ownedID, Kind: "manual", Expect: "人工复查（端口非法）"}
	}
	return CheckSpec{OwnedID: ownedID, Kind: kind,
		Command: fmt.Sprintf("nc -z -w5 %s %d", addr, port), Expect: "端口存活"}
}

// SafeHost 只允许域名/IP 字符，避免把台账里的脏数据拼进 shell。
func SafeHost(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_', r == ':':
		default:
			return false
		}
	}
	return true
}

// SafeURL 在 SafeHost 基础上允许 scheme 与路径（不接受空格/引号/分号等）。
func SafeURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" {
		return false
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_', r == ':', r == '/', r == '?', r == '=', r == '&', r == '%', r == '#', r == '[', r == ']':
		default:
			return false
		}
	}
	return strings.Contains(u, "://") || !strings.ContainsAny(u, " 	")
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstNonZero(vals ...int64) int64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
