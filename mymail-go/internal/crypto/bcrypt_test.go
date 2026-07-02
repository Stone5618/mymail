// Package crypto
// bcrypt_test.go 测试密码哈希与校验。
// 验证点：
//   - 哈希输出格式（$2a$ / $2b$ 前缀）
//   - 正确密码校验通过
//   - 错误密码校验失败
//   - 空密码拒绝
//   - 同一密码两次哈希结果不同（盐随机）
package crypto

import (
	"strings"
	"testing"
)

func TestHashPassword_Success(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("哈希前缀错误，期望 $2a$/$2b$，实际 %q", hash[:4])
	}
	if len(hash) < 50 {
		t.Errorf("哈希长度异常: %d", len(hash))
	}
}

func TestHashPassword_Empty(t *testing.T) {
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("空密码应返回错误")
	}
}

func TestComparePassword_Correct(t *testing.T) {
	hash, err := HashPassword("mypassword")
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}
	if err := ComparePassword(hash, "mypassword"); err != nil {
		t.Errorf("正确密码校验失败: %v", err)
	}
}

func TestComparePassword_Wrong(t *testing.T) {
	hash, err := HashPassword("mypassword")
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}
	if err := ComparePassword(hash, "wrongpassword"); err == nil {
		t.Fatal("错误密码应校验失败")
	}
}

func TestHashPassword_DifferentSalt(t *testing.T) {
	h1, _ := HashPassword("samepassword")
	h2, _ := HashPassword("samepassword")
	if h1 == h2 {
		t.Error("同一密码两次哈希应不同（盐随机）")
	}
	// 但两者都应能校验通过
	if err := ComparePassword(h1, "samepassword"); err != nil {
		t.Errorf("h1 校验失败: %v", err)
	}
	if err := ComparePassword(h2, "samepassword"); err != nil {
		t.Errorf("h2 校验失败: %v", err)
	}
}

func TestBcryptCost_Constant(t *testing.T) {
	if BcryptCost != 12 {
		t.Errorf("BcryptCost 期望 12，实际 %d", BcryptCost)
	}
}
