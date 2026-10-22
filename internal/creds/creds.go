// Package creds 是「凭据保险库 + 复用计划」：
//   - 秘密本体用 AES-256-GCM 加密后落库，报告/日志/知识库只出现引用 id；
//   - 去重用 HMAC 指纹（不是明文，也不可逆推出明文）；
//   - 复用计划把"一条口令能试哪些服务"算出来，交给 worker 执行。
//
// 纪律（docs/最终方案-v1.md ADR A11）：明文永不落库、永不进日志。
package creds

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/internal/dbiface"
)

// Vault 负责加密与指纹。密钥必须是 32 字节（AES-256）。
type Vault struct {
	aead   cipher.AEAD
	macKey []byte
}

// NewVault 从十六进制/原始字符串构造密钥；长度不足时用 SHA-256 派生 32 字节。
// 空 key 返回错误（调用方应生成一次性密钥并明确告知"重启后旧密文不可解"）。
func NewVault(key string) (*Vault, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("creds: 空密钥（请设置 ARTEX_CRED_KEY 或生成一次性密钥）")
	}
	raw, err := hex.DecodeString(strings.TrimSpace(key))
	if err != nil || len(raw) != 32 {
		sum := sha256.Sum256([]byte(key))
		raw = sum[:]
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("creds: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creds: gcm: %w", err)
	}
	mac := sha256.Sum256(append([]byte("artex-creds-fingerprint:"), raw...))
	return &Vault{aead: aead, macKey: mac[:]}, nil
}

// NewEphemeralVault 生成一次性密钥（进程内有效）。
func NewEphemeralVault() *Vault {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// 极端情况：用时间派生（仍然只影响本进程）。
		sum := sha256.Sum256([]byte(time.Now().String()))
		key = sum[:]
	}
	v, _ := NewVault(hex.EncodeToString(key))
	return v
}

// Encrypt 返回密文与 nonce。
func (v *Vault) Encrypt(plain string) (ct, nonce []byte, err error) {
	n := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, nil, fmt.Errorf("creds: nonce: %w", err)
	}
	return v.aead.Seal(nil, n, []byte(plain), nil), n, nil
}

// Decrypt 还原明文。
func (v *Vault) Decrypt(ct, nonce []byte) (string, error) {
	if len(nonce) != v.aead.NonceSize() {
		return "", errors.New("creds: nonce 长度不对")
	}
	pt, err := v.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("creds: decrypt: %w", err)
	}
	return string(pt), nil
}

// Fingerprint 返回去重指纹（HMAC-SHA256，稳定不可逆）。
func (v *Vault) Fingerprint(secret string) string {
	m := hmac.New(sha256.New, v.macKey)
	m.Write([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(m.Sum(nil))
}

// Entry 是凭据元数据（**不含明文**）。
type Entry struct {
	ID          int64     `json:"id"`
	TaskID      int64     `json:"task_id,omitempty"`
	AssetID     int64     `json:"asset_id,omitempty"`
	Kind        string    `json:"kind"` // password|hash|token|key|ssh_key|conn_str
	Username    string    `json:"username,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Verified    bool      `json:"verified"`
	Status      string    `json:"status"` // discovered|verified|in_use|rotated|invalid|archived
	UsedOn      []string  `json:"used_on,omitempty"`
	UseCount    int       `json:"use_count"`
	Source      string    `json:"source,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type memEntry struct {
	entry Entry
	ct    []byte
	nonce []byte
}

// Store 是凭据库。db 为 nil 时退化为内存实现（测试/无库场景）。
type Store struct {
	db dbiface.DB
	v  *Vault

	mu   sync.Mutex
	mem  map[string]*memEntry // fingerprint → entry
	next int64
}

func NewStore(db dbiface.DB, v *Vault) *Store {
	if v == nil {
		v = NewEphemeralVault()
	}
	return &Store{db: db, v: v, mem: map[string]*memEntry{}}
}

// Vault 返回底层加密器（测试/运维用）。
func (s *Store) Vault() *Vault { return s.v }

// Add 写入一条凭据。同指纹（同一秘密+同一用户名）不重复入库，返回 existing=true。
func (s *Store) Add(ctx context.Context, e Entry, secret string) (id int64, existing bool, err error) {
	if strings.TrimSpace(secret) == "" {
		return 0, false, errors.New("creds: 空秘密")
	}
	if e.Kind == "" {
		e.Kind = "password"
	}
	if e.Status == "" {
		e.Status = "discovered"
	}
	fp := s.v.Fingerprint(e.Kind + "|" + strings.ToLower(strings.TrimSpace(e.Username)) + "|" + secret)
	ct, nonce, err := s.v.Encrypt(secret)
	if err != nil {
		return 0, false, err
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if cur, ok := s.mem[fp]; ok {
			return cur.entry.ID, true, nil
		}
		s.next++
		e.ID = s.next
		e.Fingerprint = fp
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now()
		}
		s.mem[fp] = &memEntry{entry: e, ct: ct, nonce: nonce}
		return e.ID, false, nil
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO creds_vault (task_id, asset_id, kind, username, secret_enc, secret_nonce, fingerprint, verified, status, source)
		 VALUES (NULLIF($1,0), NULLIF($2,0), $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (fingerprint) WHERE fingerprint <> '' DO NOTHING
		 RETURNING id`,
		e.TaskID, e.AssetID, e.Kind, e.Username, ct, nonce, fp, e.Verified, e.Status, e.Source)
	var newID int64
	if err := row.Scan(&newID); err != nil {
		// 冲突（已存在）→ 查回 id。
		if qerr := s.db.QueryRowContext(ctx, `SELECT id FROM creds_vault WHERE fingerprint = $1`, fp).Scan(&newID); qerr == nil {
			return newID, true, nil
		}
		return 0, false, fmt.Errorf("creds: insert: %w", err)
	}
	return newID, false, nil
}

// Get 取凭据元数据与明文（明文只交给调用方，绝不落日志）。
func (s *Store) Get(ctx context.Context, id int64) (Entry, string, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range s.mem {
			if m.entry.ID == id {
				pt, err := s.v.Decrypt(m.ct, m.nonce)
				return m.entry, pt, err
			}
		}
		return Entry{}, "", fmt.Errorf("creds: id %d 不存在", id)
	}
	var e Entry
	var ct, nonce []byte
	var usedOnJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(task_id,0), COALESCE(asset_id,0), kind, username, verified, status,
		        COALESCE(array_to_json(used_on)::text,'[]'), use_count, source, created_at, secret_enc, secret_nonce
		 FROM creds_vault WHERE id = $1`, id).
		Scan(&e.ID, &e.TaskID, &e.AssetID, &e.Kind, &e.Username, &e.Verified, &e.Status, &usedOnJSON, &e.UseCount, &e.Source, &e.CreatedAt, &ct, &nonce)
	_ = json.Unmarshal([]byte(usedOnJSON), &e.UsedOn)
	if err != nil {
		return Entry{}, "", fmt.Errorf("creds: get %d: %w", id, err)
	}
	pt, err := s.v.Decrypt(ct, nonce)
	return e, pt, err
}

// List 列出某任务的凭据元数据（不含明文）。
func (s *Store) List(ctx context.Context, taskID int64, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := make([]Entry, 0, len(s.mem))
		for _, m := range s.mem {
			if taskID > 0 && m.entry.TaskID != taskID {
				continue
			}
			out = append(out, m.entry)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		if len(out) > limit {
			out = out[:limit]
		}
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(task_id,0), COALESCE(asset_id,0), kind, username, verified, status,
		        COALESCE(array_to_json(used_on)::text,'[]'), use_count, source, created_at
		 FROM creds_vault WHERE ($1 = 0 OR task_id = $1) ORDER BY id DESC LIMIT $2`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("creds: list: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var usedOnJSON string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AssetID, &e.Kind, &e.Username, &e.Verified, &e.Status, &usedOnJSON, &e.UseCount, &e.Source, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(usedOnJSON), &e.UsedOn)
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkUsed 记录"用在了哪、用了几次"。
func (s *Store) MarkUsed(ctx context.Context, id int64, target string) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range s.mem {
			if m.entry.ID == id {
				m.entry.UseCount++
				if target != "" {
					m.entry.UsedOn = append(m.entry.UsedOn, target)
				}
				if m.entry.Status == "discovered" {
					m.entry.Status = "in_use"
				}
				return nil
			}
		}
		return fmt.Errorf("creds: id %d 不存在", id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE creds_vault SET use_count = use_count + 1,
		 used_on = CASE WHEN $2 = '' THEN used_on ELSE array_append(used_on, $2) END,
		 status = CASE WHEN status = 'discovered' THEN 'in_use' ELSE status END
		 WHERE id = $1`, id, target)
	return err
}

// SetVerified 标记凭据是否可用。
func (s *Store) SetVerified(ctx context.Context, id int64, ok bool) error {
	status := "invalid"
	if ok {
		status = "verified"
	}
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range s.mem {
			if m.entry.ID == id {
				m.entry.Verified = ok
				m.entry.Status = status
				return nil
			}
		}
		return fmt.Errorf("creds: id %d 不存在", id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE creds_vault SET verified = $2, status = $3 WHERE id = $1`, id, ok, status)
	return err
}

// Target 是一个待尝试的服务端点。
type Target struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Attempt 是一次凭据复用尝试。
type Attempt struct {
	Target   Target `json:"target"`
	Proto    string `json:"proto"`
	Command  string `json:"command"` // 可直接执行的模板（不含明文，占位 ${USER}/${SECRET}）
	Why      string `json:"why"`
	Priority int    `json:"priority"`
}

// servicePorts 是凭据复用优先尝试的服务（越靠前越值得试）。
var servicePorts = []struct {
	port  int
	proto string
	cmd   string
}{
	{22, "ssh", `sshpass -p '${SECRET}' ssh -o StrictHostKeyChecking=no ${USER}@${HOST}`},
	{445, "smb", `nxc smb ${HOST} -u '${USER}' -p '${SECRET}'`},
	{5985, "winrm", `nxc winrm ${HOST} -u '${USER}' -p '${SECRET}'`},
	{3306, "mysql", `mysql -h ${HOST} -u '${USER}' -p'${SECRET}' -e 'SELECT VERSION()'`},
	{5432, "postgres", `PGPASSWORD='${SECRET}' psql -h ${HOST} -U '${USER}' -c 'SELECT version()'`},
	{1433, "mssql", `nxc mssql ${HOST} -u '${USER}' -p '${SECRET}'`},
	{6379, "redis", `redis-cli -h ${HOST} -a '${SECRET}' PING`},
	{3389, "rdp", `nxc rdp ${HOST} -u '${USER}' -p '${SECRET}'`},
	{27017, "mongo", `mongosh "mongodb://${USER}:${SECRET}@${HOST}:27017/admin" --eval 'db.version()'`},
}

// PlanReuse 给出"这条凭据能试哪些服务"的尝试列表（按端口价值排序）。
// ports 为空时按全量清单，调用方可用已扫描到的开放端口收窄。
func PlanReuse(t Target, e Entry, ports []int) []Attempt {
	open := map[int]bool{}
	for _, p := range ports {
		open[p] = true
	}
	var out []Attempt
	for i, sp := range servicePorts {
		if len(open) > 0 && !open[sp.port] {
			continue
		}
		if e.Kind == "ssh_key" && sp.port != 22 {
			continue
		}
		if e.Kind == "hash" && sp.port != 445 && sp.port != 3389 {
			continue // hash 只用于 Windows 认证
		}
		prio := 10 - i
		why := fmt.Sprintf("%s 服务（%d）支持用户名+口令认证", sp.proto, sp.port)
		if e.Verified {
			why = "该凭据已验证过，优先复用（" + why + "）"
			prio += 5
		}
		out = append(out, Attempt{
			Target: Target{Host: t.Host, Port: sp.port}, Proto: sp.proto,
			Command: sp.cmd, Why: why, Priority: prio,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// WebPorts 是后台/HTTP Basic 复用会尝试的端口。
var WebPorts = []int{80, 443, 8080, 8443}

// PlanWebReuse 生成后台口令复用尝试（HTTP Basic 只验证认证是否通过，不读数据）。
func PlanWebReuse(host string, ports []int) []Attempt {
	list := ports
	if len(list) == 0 {
		list = WebPorts
	}
	var out []Attempt
	for _, p := range list {
		ok := false
		for _, w := range WebPorts {
			if p == w {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		out = append(out, Attempt{
			Target: Target{Host: host, Port: p}, Proto: "httpbasic",
			Command: "/", Why: "后台/HTTP Basic 认证验证（只看状态码，不读数据）", Priority: 40,
		})
	}
	return out
}
