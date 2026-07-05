// Package maildir 实现 Maildir 格式的邮件文件存储。
//
// Maildir 结构（与原 Node.js 一致）：
//   {maildirPath}/{domain}/{username}/
//     ├── new/   已接收未读
//     ├── cur/   已读
//     └── tmp/   临时（写入中）
//
// 命名约定：{timestamp}.{unique}.{domain}
// 与原 Node.js smtp-receiver.js 完全兼容。
package maildir

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mymail/mymail-go/internal/config"
)

// Maildir 封装 Maildir 文件操作。
type Maildir struct {
	rootPath string
	domain   string
}

// New 创建 Maildir 实例。
// rootPath: 配置 cfg.MaildirPath
// domain: 配置 cfg.Domain
func New(cfg *config.Config) *Maildir {
	return &Maildir{
		rootPath: cfg.MaildirPath,
		domain:   cfg.Domain,
	}
}

// userDir 返回用户 maildir 根目录。
// 结构：{root}/{domain}/{username}
func (m *Maildir) userDir(username string) string {
	return filepath.Join(m.rootPath, m.domain, username)
}

// subDir 返回用户 maildir 子目录（new/cur/tmp）。
func (m *Maildir) subDir(username, sub string) string {
	return filepath.Join(m.userDir(username), sub)
}

// EnsureUserDirs 创建用户的 new/cur/tmp 三个子目录。
// 目录所有者固定为 1000:1000，与 Dovecot 容器内 vmail 用户保持一致。
func (m *Maildir) EnsureUserDirs(username string) error {
	base := m.userDir(username)
	if err := os.MkdirAll(base, 0755); err != nil {
		return fmt.Errorf("创建 maildir 基础目录 %s 失败: %w", base, err)
	}
	_ = os.Chown(base, 1000, 1000)
	for _, sub := range []string{"new", "cur", "tmp"} {
		dir := m.subDir(username, sub)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("创建 maildir 目录 %s 失败: %w", dir, err)
		}
		_ = os.Chown(dir, 1000, 1000)
	}
	return nil
}

// SaveNew 将原始邮件数据保存到 new/ 目录。
// 返回文件名（不含路径）。
// 与原 Node.js 一致：{timestamp}.{unique8}.{domain}
// 文件所有者固定为 1000:1000，确保 Dovecot 容器内 vmail 用户可读取。
func (m *Maildir) SaveNew(username string, data []byte) (string, error) {
	if err := m.EnsureUserDirs(username); err != nil {
		return "", err
	}
	filename := m.genFilename()
	path := filepath.Join(m.subDir(username, "new"), filename)
	if err := os.WriteFile(path, data, 0640); err != nil {
		return "", fmt.Errorf("写入 maildir 文件失败: %w", err)
	}
	if err := os.Chown(path, 1000, 1000); err != nil {
		// 非 Linux 环境可能失败，仅记录警告，不影响投递。
		fmt.Fprintf(os.Stderr, "设置 maildir 文件所有者失败 %s: %v\n", path, err)
	}
	return filename, nil
}

// genFilename 生成 maildir 文件名：{timestamp}.{unique8}.{domain}
func (m *Maildir) genFilename() string {
	ts := time.Now().Unix()
	unique := randomHex(4) // 8 个 hex 字符
	return fmt.Sprintf("%d.%s.%s", ts, unique, m.domain)
}

// randomHex 生成 n 字节的随机 hex 字符串。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// rand.Read 极少失败，降级用时间戳
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// FullPath 返回 new/ 目录下指定文件的完整路径。
func (m *Maildir) FullPath(username, filename string) string {
	return filepath.Join(m.subDir(username, "new"), filename)
}

// Delete 删除 new/ 目录下指定文件。
// 文件不存在不报错（幂等）。
func (m *Maildir) Delete(username, filename string) error {
	path := m.FullPath(username, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 maildir 文件失败: %w", err)
	}
	return nil
}

// MoveToCur 将文件从 new/ 移到 cur/（标记已读）。
// 文件不存在不报错（幂等）。
func (m *Maildir) MoveToCur(username, filename string) error {
	src := filepath.Join(m.subDir(username, "new"), filename)
	dst := filepath.Join(m.subDir(username, "cur"), filename)
	if err := os.Rename(src, dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("移动 maildir 文件到 cur 失败: %w", err)
	}
	return nil
}

// Exists 检查用户 maildir 是否存在。
func (m *Maildir) Exists(username string) bool {
	info, err := os.Stat(m.userDir(username))
	return err == nil && info.IsDir()
}

// UserUsedBytes 返回用户 maildir 总字节数（new + cur + tmp）。
// 用于存储配额校验。
func (m *Maildir) UserUsedBytes(username string) (int64, error) {
	var total int64
	for _, sub := range []string{"new", "cur", "tmp"} {
		size, err := dirSize(m.subDir(username, sub))
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

// dirSize 递归计算目录大小。
func dirSize(path string) (int64, error) {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			sub, err := dirSize(filepath.Join(path, entry.Name()))
			if err != nil {
				return 0, err
			}
			size += sub
		} else {
			info, err := entry.Info()
			if err != nil {
				return 0, err
			}
			size += info.Size()
		}
	}
	return size, nil
}
