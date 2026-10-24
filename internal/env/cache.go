package env

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// 缓存：<CacheDir>/tools_cache.json（内容是 []Tool）。
//
//   - 写入用"临时文件 + rename"，保证原子性（读到的永远是完整 JSON）；
//   - Load 会检查 mtime，超过 7 天视为过期 → 重新 Scan；
//   - CacheDir 为空时 Save 是 no-op，Load 直接 Scan。

// cachePath 返回缓存文件绝对路径（CacheDir 为空时返回空串）。
func (d *Discoverer) cachePath() string {
	if d.cacheDir == "" {
		return ""
	}
	return filepath.Join(d.cacheDir, cacheFileName)
}

// Save 原子写入缓存。CacheDir 为空时不做任何事。
func (d *Discoverer) Save(ctx context.Context, tools []Tool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := d.cachePath()
	if path == "" {
		return nil
	}
	if tools == nil {
		tools = []Tool{}
	}
	data, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(d.cacheDir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(d.cacheDir, "."+cacheFileName+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// rename 成功后 tmpName 已不存在，这里只是失败路径的兜底清理。
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return ctx.Err()
}

// Invalidate 删掉磁盘缓存，下一次 Load 必定重扫。
// 用途：环境页“重新扫描”按钮——用户刚 apt 装完工具时不用重启进程。
func (d *Discoverer) Invalidate() error {
	if d.cacheDir == "" {
		return nil
	}
	if err := os.Remove(d.cachePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Load 读缓存；缓存不存在 / 损坏 / 超过 24 小时 → 重新 Scan（并回写缓存）。
//
// 2026-10-24 改：TTL 从 7 天降到 1 天。实测教训：用户 apt 装完工具后环境页
// 仍然说“缺 X”，因为缓存是 7 天前的，而环境页只看缓存 → 人会以为装失败了。
func (d *Discoverer) Load(ctx context.Context) ([]Tool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.cacheDir == "" {
		return d.Scan(ctx)
	}

	path := d.cachePath()
	if data, err := os.ReadFile(path); err == nil {
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) <= cacheTTL {
			var tools []Tool
			if json.Unmarshal(data, &tools) == nil {
				if tools == nil {
					tools = []Tool{}
				}
				return tools, nil
			}
			// JSON 损坏：当作过期处理，走重扫。
		}
	}

	tools, err := d.Scan(ctx)
	if err != nil {
		return nil, err
	}
	// 回写失败不影响结果：缓存只是加速手段。
	_ = d.Save(ctx, tools)
	return tools, nil
}
