package creds

// 凭据主密钥的落盘兜底：环境变量 > <dataDir>/cred.key > 首次生成。
//
// 设计要点（docs/02-P8执行手册.md P8-7）：
//   - 绝不覆盖已有 cred.key（覆盖会让旧密文永久不可解）；
//   - 生成用 O_EXCL，天然处理"两个进程同时首启"的竞争；
//   - Windows 上 0600 不生效，由用户 profile 目录（data/ 默认在用户目录下）
//     的权限兜底，因此这里不把它当成错误。

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// KeyEnvVar 是凭据主密钥的环境变量名（优先级最高）。
	KeyEnvVar = "ARTEX_CRED_KEY"
	// KeyFileName 是 dataDir 下的密钥文件名。
	KeyFileName = "cred.key"
)

// ErrKeyFileInvalid 表示已有 cred.key 的内容不是 64 位 hex。
// 调用方应显式告警，但绝不能静默重新生成（否则旧密文全废）。
var ErrKeyFileInvalid = errors.New("cred.key 内容非法（应为 64 位 hex）")

// LoadOrCreateKey 按优先级取凭据主密钥：
//  1. 环境变量 ARTEX_CRED_KEY 非空 → 直接返回它（原样，不做格式校验，交给 NewVault 判断）
//  2. <dataDir>/cred.key 存在 → 读出 trim 后的内容
//  3. 都不存在 → crypto/rand 生成 32 字节，hex 编码（64 字符），以 0600 写入文件后返回
//
// 返回值 fp 是密钥指纹（sha256(key) 的前 8 位 hex），供页面显示
// "凭据加密已启用（指纹 xxxx）"。
func LoadOrCreateKey(dataDir string) (key, fp string, err error) {
	// 1) 环境变量优先；此时不创建目录、不落盘。
	if env := os.Getenv(KeyEnvVar); strings.TrimSpace(env) != "" {
		return env, Fingerprint(env), nil
	}

	// 2) 目录不存在时创建（仅 dataDir/cred.key 路径需要）。
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", "", fmt.Errorf("creds: 创建密钥目录 %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, KeyFileName)
	if k, rerr := readKeyFile(path, false); rerr == nil {
		return k, Fingerprint(k), nil
	} else if !errors.Is(rerr, os.ErrNotExist) {
		return "", "", rerr
	}

	// 3) 生成新密钥。注意：os.WriteFile 不能用于这里，它会覆盖已有文件。
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("creds: 生成随机密钥: %w", err)
	}
	hexKey := hex.EncodeToString(raw)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// 并发首启：另一个进程赢了。读它写好的 key，绝不覆盖。
			// 它可能刚创建文件、还没写入，readKeyFile 会短暂等待非空内容。
			k, rerr := readKeyFile(path, true)
			if rerr != nil {
				return "", "", rerr
			}
			return k, Fingerprint(k), nil
		}
		return "", "", fmt.Errorf("creds: 创建 %s: %w", path, err)
	}
	if _, werr := f.WriteString(hexKey + "\n"); werr != nil {
		_ = f.Close()
		return "", "", fmt.Errorf("creds: 写入 %s: %w", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		return "", "", fmt.Errorf("creds: 关闭 %s: %w", path, cerr)
	}
	// Windows 上 0600 不生效（os.Chmod 实际是 no-op），权限由用户 profile
	// 目录兜底；这里不把 chmod 失败当成错误。
	_ = os.Chmod(path, 0o600)

	return hexKey, Fingerprint(hexKey), nil
}

// Fingerprint 计算任意 key 字符串的 8 位指纹（空串返回 "none"）。
func Fingerprint(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:8]
}

// readKeyFile 读取并校验 cred.key。waitForWrite 只用于"别人刚 O_EXCL 创建、
// 可能还没写完"的竞争窗口：仅当文件为空时短暂重试，内容非空但非法则立即报错。
func readKeyFile(path string, waitForWrite bool) (string, error) {
	const (
		waitTotal = 500 * time.Millisecond
		waitStep  = 10 * time.Millisecond
	)
	deadline := time.Now().Add(waitTotal)
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("creds: 读取 %s: %w", path, err)
		}
		key := strings.TrimSpace(string(data))
		if key != "" && len(key) == 64 {
			if _, err := hex.DecodeString(key); err == nil {
				return key, nil
			}
		}
		if !waitForWrite || time.Now().After(deadline) {
			if waitForWrite {
				return "", fmt.Errorf("%w: %s（等待写入超时）", ErrKeyFileInvalid, path)
			}
			if key == "" {
				return "", fmt.Errorf("%w: %s（文件为空）", ErrKeyFileInvalid, path)
			}
			return "", fmt.Errorf("%w: %s", ErrKeyFileInvalid, path)
		}
		time.Sleep(waitStep)
	}
}
