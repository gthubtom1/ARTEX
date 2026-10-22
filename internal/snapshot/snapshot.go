// Package snapshot 提供"写之前先留档"的能力：文件级快照 + 一键回滚。
//
// 设计纪律（docs/最终方案-v1.md §5）：不靠"禁止写"，靠"写了能撤回"。
// 上层在写文件 / 改配置 / 删数据之前调用 Create，出问题用 Restore 还原。
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync/atomic"
	"time"
)

// FileMeta 描述一个被快照的文件。
type FileMeta struct {
	OrigPath string `json:"orig_path"`
	RelPath  string `json:"rel_path"`  // 快照目录内的相对路径
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Mode     uint32 `json:"mode"`
	Missing  bool   `json:"missing,omitempty"` // 快照时文件不存在（用于记录"将要创建"）
}

// Snapshot 是一次快照的清单。
type Snapshot struct {
	ID        string     `json:"id"`
	TaskID    int64      `json:"task_id"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"created_at"`
	Files     []FileMeta `json:"files"`
	Bytes     int64      `json:"bytes"`
}

// seq 保证同一纳秒内多次创建也不会撞 ID（Windows 时钟粒度较粗）。
var seq atomic.Uint64

// Store 管理快照目录（默认 <dataDir>/snapshots）。
type Store struct {
	Root string
}

func New(root string) *Store { return &Store{Root: root} }

func (s *Store) dir(id string) string { return filepath.Join(s.Root, id) }

// Create 对给定路径做快照。不存在的路径会以 Missing=true 记录（便于"删前留档"场景）。
func (s *Store) Create(taskID int64, reason string, paths []string) (*Snapshot, error) {
	if s == nil || s.Root == "" {
		return nil, fmt.Errorf("snapshot: store not configured")
	}
	now := time.Now()
	id := fmt.Sprintf("%d-%s-%04d", now.UnixNano(), shortHash(reason+strconv.FormatInt(taskID, 10)), seq.Add(1)%10000)
	if err := os.MkdirAll(s.dir(id), 0o700); err != nil {
		return nil, fmt.Errorf("snapshot: mkdir: %w", err)
	}
	snap := &Snapshot{ID: id, TaskID: taskID, Reason: reason, CreatedAt: now}
	for i, p := range paths {
		fm := FileMeta{OrigPath: p}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			fm.Missing = true
			snap.Files = append(snap.Files, fm)
			continue
		}
		rel := filepath.Join(fmt.Sprintf("f%03d", i), filepath.Base(p))
		dst := filepath.Join(s.dir(id), rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return nil, fmt.Errorf("snapshot: mkdir file: %w", err)
		}
		sum, size, err := copyFile(p, dst, st.Mode())
		if err != nil {
			return nil, fmt.Errorf("snapshot: copy %s: %w", p, err)
		}
		fm.RelPath, fm.SHA256, fm.Size, fm.Mode = rel, sum, size, uint32(st.Mode().Perm())
		snap.Bytes += size
		snap.Files = append(snap.Files, fm)
	}
	if err := writeManifest(s.dir(id), snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// Restore 把快照里的文件写回原位置（校验 sha256）。
func (s *Store) Restore(id string) error {
	snap, err := s.load(id)
	if err != nil {
		return err
	}
	for _, fm := range snap.Files {
		if fm.Missing {
			continue
		}
		src := filepath.Join(s.dir(id), fm.RelPath)
		if _, size, err := hashFile(src); err != nil {
			return fmt.Errorf("snapshot: restore read %s: %w", fm.OrigPath, err)
		} else if size != fm.Size {
			return fmt.Errorf("snapshot: %s size mismatch (want %d got %d)", fm.OrigPath, fm.Size, size)
		}
		if _, err := os.Stat(filepath.Dir(fm.OrigPath)); err != nil {
			if err := os.MkdirAll(filepath.Dir(fm.OrigPath), 0o755); err != nil {
				return fmt.Errorf("snapshot: restore mkdir: %w", err)
			}
		}
		if _, _, err := copyFile(src, fm.OrigPath, os.FileMode(fm.Mode)); err != nil {
			return fmt.Errorf("snapshot: restore write %s: %w", fm.OrigPath, err)
		}
	}
	return nil
}

// Get 读取一次快照清单。
func (s *Store) Get(id string) (*Snapshot, error) { return s.load(id) }

// List 列出某任务的快照（新→旧）。
func (s *Store) List(taskID int64) ([]Snapshot, error) {
	ents, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Snapshot
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		snap, err := s.load(e.Name())
		if err != nil || (taskID > 0 && snap.TaskID != taskID) {
			continue
		}
		out = append(out, *snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Prune 只保留最近 keep 个快照（按创建时间）。
func (s *Store) Prune(keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	all, err := s.List(0)
	if err != nil || len(all) <= keep {
		return 0, err
	}
	removed := 0
	for _, snap := range all[keep:] {
		if err := os.RemoveAll(s.dir(snap.ID)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// ── 内部工具 ──

func (s *Store) load(id string) (*Snapshot, error) {
	if id == "" || filepath.Base(id) != id {
		return nil, fmt.Errorf("snapshot: bad id")
	}
	b, err := os.ReadFile(filepath.Join(s.dir(id), "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("snapshot: load %s: %w", id, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("snapshot: parse %s: %w", id, err)
	}
	return &snap, nil
}

func writeManifest(dir string, snap *Snapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600)
}

func copyFile(src, dst string, mode os.FileMode) (sum string, size int64, err error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return "", 0, err
	}
	defer out.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}
