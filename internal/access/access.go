// Package access 是「立足点池」：一个任务里所有 worker 共享已拿到的会话。
//
// 解决的痛点（docs/最终方案-v1.md §2）：原来 shell 不跨 worker——
// A 打下来的 shell，B 用不了，于是每个 worker 都得重新打一遍。
// 这里只做**登记与租借**（不含明文凭据；凭据在 creds 包里按 id 引用）。
package access

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Session 是一个可利用的立足点。
type Session struct {
	ID         int64     `json:"id"`
	OwnedID    int64     `json:"owned_id"` // assets_owned.id（0 = 未登记台账）
	Proto      string    `json:"proto"`    // shell|webshell|ssh|vpn|jump|admin|db
	Endpoint   string    `json:"endpoint"` // host:port 或 url
	Host       string    `json:"host,omitempty"`
	Port       int       `json:"port,omitempty"`
	User       string    `json:"user,omitempty"`
	CmdParam   string    `json:"cmd_param,omitempty"` // webshell 的参数名（默认 cmd）
	CredRef    int64     `json:"cred_ref,omitempty"`
	Worker     string    `json:"worker,omitempty"` // 当前租用人
	LeaseUntil time.Time `json:"lease_until,omitempty"`
	Status     string    `json:"status"` // up|down
	CreatedAt  time.Time `json:"created_at"`
	Notes      string    `json:"notes,omitempty"`
	UseCount   int       `json:"use_count"`
}

// Pool 是进程内会话池（按任务一个实例；进程重启后由台账复查重建）。
type Pool struct {
	mu      sync.Mutex
	byID    map[int64]*Session
	next    int64
	ttl     time.Duration
	nowFunc func() time.Time
}

// NewPool 创建池；ttl 为租约时长（0 → 10 分钟）。
func NewPool(ttl time.Duration) *Pool {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Pool{byID: map[int64]*Session{}, ttl: ttl, nowFunc: time.Now}
}

// Register 登记一个会话；同一 owned+proto+endpoint 只登记一次，返回已有 id。
func (p *Pool) Register(s Session) (int64, error) {
	if s.Proto == "" || s.Endpoint == "" {
		return 0, fmt.Errorf("access: proto 与 endpoint 必填")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, cur := range p.byID {
		if cur.OwnedID == s.OwnedID && cur.Proto == s.Proto && cur.Endpoint == s.Endpoint {
			cur.Status = "up"
			return cur.ID, nil
		}
	}
	p.next++
	s.ID = p.next
	if s.Status == "" {
		s.Status = "up"
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = p.now()
	}
	cp := s
	p.byID[s.ID] = &cp
	return s.ID, nil
}

// Lease 把一个可用会话租给某个 worker。优先挑：未被租 / 租约已过期，且用得最少的。
// ownedID=0 表示"任意一个可用会话"。
func (p *Pool) Lease(worker string, ownedID int64) (*Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	var cands []*Session
	for _, s := range p.byID {
		if s.Status != "up" {
			continue
		}
		if ownedID != 0 && s.OwnedID != ownedID {
			continue
		}
		if s.Worker != "" && s.Worker != worker && s.LeaseUntil.After(now) {
			continue // 别人正在用
		}
		cands = append(cands, s)
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("access: 没有可用会话（owned=%d）", ownedID)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].UseCount != cands[j].UseCount {
			return cands[i].UseCount < cands[j].UseCount
		}
		return cands[i].ID < cands[j].ID
	})
	pick := cands[0]
	pick.Worker = worker
	pick.LeaseUntil = now.Add(p.ttl)
	pick.UseCount++
	cp := *pick
	return &cp, nil
}

// Release 释放租约（会话保持 up，别人可继续用）。
func (p *Pool) Release(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.byID[id]; ok {
		s.Worker = ""
		s.LeaseUntil = time.Time{}
	}
}

// Mark 改状态（down 后不会再被租出）。
func (p *Pool) Mark(id int64, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.byID[id]; ok {
		s.Status = status
		if status != "up" {
			s.Worker = ""
			s.LeaseUntil = time.Time{}
		}
	}
}

// Get 取单个会话。
func (p *Pool) Get(id int64) (*Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.byID[id]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

// List 列出会话（ownedID=0 → 全部）。
func (p *Pool) List(ownedID int64) []Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Session, 0, len(p.byID))
	for _, s := range p.byID {
		if ownedID != 0 && s.OwnedID != ownedID {
			continue
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PurgeDown 清掉 down 的会话（战后清理用）。
func (p *Pool) PurgeDown() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for id, s := range p.byID {
		if s.Status != "up" {
			delete(p.byID, id)
			n++
		}
	}
	return n
}

func (p *Pool) now() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

// Restore 用持久化记录恢复一个会话（保留 id；已被占用则重新分配）。
func (p *Pool) Restore(s Session) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.ID > 0 {
		if _, exists := p.byID[s.ID]; !exists {
			cp := s
			if cp.Status == "" {
				cp.Status = "up"
			}
			if cp.CreatedAt.IsZero() {
				cp.CreatedAt = p.now()
			}
			p.byID[s.ID] = &cp
			if s.ID > p.next {
				p.next = s.ID
			}
			return s.ID
		}
	}
	p.mu.Unlock()
	id, _ := p.Register(s)
	p.mu.Lock()
	return id
}
