// Package maildir
// maildir_test.go 测试 Maildir 文件存储操作。
//
// 测试覆盖：
//   - EnsureUserDirs 创建目录
//   - SaveNew 保存文件并校验文件名格式
//   - FullPath/Delete/MoveToCur 文件操作
//   - Exists 检查用户目录
//   - UserUsedBytes 统计大小
package maildir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mymail/mymail-go/internal/config"
)

// newTestMaildir 创建基于 t.TempDir() 的测试 Maildir。
func newTestMaildir(t *testing.T) *Maildir {
	t.Helper()
	cfg := &config.Config{
		MaildirPath: filepath.Join(t.TempDir(), "maildir"),
		Domain:      "example.com",
	}
	return New(cfg)
}

func TestMaildir_EnsureUserDirs(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"

	if err := m.EnsureUserDirs(username); err != nil {
		t.Fatalf("EnsureUserDirs 失败: %v", err)
	}

	// 校验三个子目录均存在
	for _, sub := range []string{"new", "cur", "tmp"} {
		dir := m.subDir(username, sub)
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("子目录 %s 不存在: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("子目录 %s 不是目录", sub)
		}
	}
}

func TestMaildir_EnsureUserDirs_Idempotent(t *testing.T) {
	m := newTestMaildir(t)
	username := "bob"

	// 重复调用不应报错
	if err := m.EnsureUserDirs(username); err != nil {
		t.Fatalf("第一次 EnsureUserDirs 失败: %v", err)
	}
	if err := m.EnsureUserDirs(username); err != nil {
		t.Fatalf("第二次 EnsureUserDirs 失败: %v", err)
	}
}

func TestMaildir_SaveNew(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"
	data := []byte("From: sender@example.com\r\nTo: alice@example.com\r\nSubject: Test\r\n\r\nHello")

	filename, err := m.SaveNew(username, data)
	if err != nil {
		t.Fatalf("SaveNew 失败: %v", err)
	}
	if filename == "" {
		t.Fatal("filename 不能为空")
	}

	// 校验文件名格式：{timestamp}.{unique8}.{domain}
	// domain 可含 .（如 example.com），故用 SplitN 切前两段
	parts := strings.SplitN(filename, ".", 3)
	if len(parts) != 3 {
		t.Fatalf("文件名格式错误：%q，期望 {ts}.{unique8}.{domain}", filename)
	}
	if parts[2] != "example.com" {
		t.Errorf("文件名 domain 部分期望 example.com，实际 %s", parts[2])
	}
	if len(parts[1]) != 8 {
		t.Errorf("unique 部分应为 8 个 hex 字符，实际 %d 个：%q", len(parts[1]), parts[1])
	}

	// 校验文件存在且内容一致
	full := m.FullPath(username, filename)
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("读取保存的文件失败: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("文件内容不一致")
	}
}

func TestMaildir_SaveNew_MultipleUnique(t *testing.T) {
	// 连续保存多个文件，文件名应唯一
	m := newTestMaildir(t)
	username := "alice"
	data := []byte("test")

	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		filename, err := m.SaveNew(username, data)
		if err != nil {
			t.Fatalf("SaveNew 第 %d 次失败: %v", i, err)
		}
		if seen[filename] {
			t.Errorf("第 %d 次保存产生重复文件名：%q", i, filename)
		}
		seen[filename] = true
	}
}

func TestMaildir_Delete(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"
	data := []byte("hello")

	filename, _ := m.SaveNew(username, data)

	// 删除存在的文件
	if err := m.Delete(username, filename); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	// 删除后文件应不存在
	if _, err := os.Stat(m.FullPath(username, filename)); !os.IsNotExist(err) {
		t.Errorf("删除后文件仍存在")
	}
}

func TestMaildir_Delete_NotExist(t *testing.T) {
	m := newTestMaildir(t)
	// 删除不存在的文件不应报错（幂等）
	if err := m.Delete("alice", "nonexistent.eml"); err != nil {
		t.Errorf("删除不存在的文件不应报错: %v", err)
	}
}

func TestMaildir_MoveToCur(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"
	data := []byte("test content")

	filename, _ := m.SaveNew(username, data)

	// 文件应在 new/
	newPath := m.FullPath(username, filename)
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new/ 中文件应存在: %v", err)
	}

	// 移到 cur/
	if err := m.MoveToCur(username, filename); err != nil {
		t.Fatalf("MoveToCur 失败: %v", err)
	}

	// new/ 中应不存在
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("移动后 new/ 中文件应不存在")
	}

	// cur/ 中应存在
	curPath := filepath.Join(m.subDir(username, "cur"), filename)
	if _, err := os.Stat(curPath); err != nil {
		t.Errorf("cur/ 中文件应存在: %v", err)
	}
}

func TestMaildir_MoveToCur_NotExist(t *testing.T) {
	m := newTestMaildir(t)
	// 移动不存在的文件不应报错（幂等）
	if err := m.MoveToCur("alice", "nonexistent.eml"); err != nil {
		t.Errorf("移动不存在的文件不应报错: %v", err)
	}
}

func TestMaildir_Exists(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"

	// 未创建前应不存在
	if m.Exists(username) {
		t.Error("未创建前 Exists 应返回 false")
	}

	// 创建后应存在
	m.EnsureUserDirs(username)
	if !m.Exists(username) {
		t.Error("创建后 Exists 应返回 true")
	}
}

func TestMaildir_UserUsedBytes(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"

	// 初始为 0
	size, err := m.UserUsedBytes(username)
	if err != nil {
		t.Fatalf("UserUsedBytes 失败: %v", err)
	}
	if size != 0 {
		t.Errorf("初始大小应为 0，实际 %d", size)
	}

	// 保存两个文件
	data1 := []byte("hello world")         // 11 字节
	data2 := []byte("another test content") // 20 字节
	m.SaveNew(username, data1)
	m.SaveNew(username, data2)

	size, err = m.UserUsedBytes(username)
	if err != nil {
		t.Fatalf("UserUsedBytes 失败: %v", err)
	}
	if size != int64(len(data1)+len(data2)) {
		t.Errorf("总大小应为 %d，实际 %d", len(data1)+len(data2), size)
	}
}

func TestMaildir_UserUsedBytes_WithCur(t *testing.T) {
	// 验证 new + cur 的总大小统计
	m := newTestMaildir(t)
	username := "alice"

	data := []byte("hello") // 5 字节
	f1, _ := m.SaveNew(username, data)
	m.SaveNew(username, data)

	// 把第一个移到 cur/
	m.MoveToCur(username, f1)

	size, _ := m.UserUsedBytes(username)
	if size != 10 {
		t.Errorf("new + cur 总大小应为 10，实际 %d", size)
	}
}

func TestMaildir_FullPath(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"
	filename := "1234567890.abcdef12.example.com"

	got := m.FullPath(username, filename)
	expected := filepath.Join(m.rootPath, m.domain, username, "new", filename)
	if got != expected {
		t.Errorf("FullPath 期望 %q，实际 %q", expected, got)
	}
}

// dirSize 递归子目录分支覆盖
func TestDirSize_WithSubdirectories(t *testing.T) {
	m := newTestMaildir(t)
	username := "alice"
	m.EnsureUserDirs(username)

	// 在 new/ 下手工创建一个子目录并放入文件（虽然 maildir 正常不会有子目录，
	// 但 dirSize 应支持递归统计）
	newDir := m.subDir(username, "new")
	subDir := filepath.Join(newDir, "subdir")
	if err := os.MkdirAll(subDir, 0750); err != nil {
		t.Fatalf("创建子目录失败: %v", err)
	}
	// 子目录中放一个 10 字节文件
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("0123456789"), 0640); err != nil {
		t.Fatalf("写入嵌套文件失败: %v", err)
	}
	// new/ 顶层放一个 5 字节文件
	if err := os.WriteFile(filepath.Join(newDir, "top.txt"), []byte("abcde"), 0640); err != nil {
		t.Fatalf("写入顶层文件失败: %v", err)
	}

	size, err := dirSize(newDir)
	if err != nil {
		t.Fatalf("dirSize 失败: %v", err)
	}
	if size != 15 {
		t.Errorf("递归总大小应为 15，实际 %d", size)
	}
}

// dirSize 对不存在目录返回 0
func TestDirSize_NotExist(t *testing.T) {
	size, err := dirSize(filepath.Join(os.TempDir(), "mymail-nonexistent-"+randomHex(4)))
	if err != nil {
		t.Fatalf("dirSize 不存在目录不应报错: %v", err)
	}
	if size != 0 {
		t.Errorf("不存在目录大小应为 0，实际 %d", size)
	}
}

// SaveNew 在 EnsureUserDirs 失败时应返回错误（路径不可写）
func TestSaveNew_EnsureDirFailure(t *testing.T) {
	// 用一个文件路径作为 rootPath，使 MkdirAll 失败
	cfg := &config.Config{
		MaildirPath: filepath.Join(t.TempDir(), "afile"), // 先创建为文件
		Domain:      "example.com",
	}
	// 把 afile 创建为文件，使后续 MkdirAll 失败
	if err := os.WriteFile(cfg.MaildirPath, []byte("x"), 0640); err != nil {
		t.Fatalf("创建占位文件失败: %v", err)
	}
	m := New(cfg)
	_, err := m.SaveNew("alice", []byte("data"))
	if err == nil {
		t.Error("EnsureUserDirs 失败时 SaveNew 应报错")
	}
}
