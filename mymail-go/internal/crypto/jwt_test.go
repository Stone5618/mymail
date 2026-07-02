// Package crypto
// jwt_test.go 测试 JWT 生成、校验、过期、密钥轮换。
// 验证点：
//   - 生成 token 非空且包含三段
//   - 正确 token 校验通过，claims 字段正确
//   - 过期 token 校验失败
//   - 篡改 token 校验失败
//   - 密钥轮换后旧 token 仍可校验
//   - 非预期算法被拒绝
//   - parseDuration 支持各种格式
package crypto

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-key-for-unit-test-only-32chars"

func TestNewJWTManager_Success(t *testing.T) {
	mgr, err := NewJWTManager(testSecret, "24h", "720h")
	if err != nil {
		t.Fatalf("NewJWTManager 失败: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager 为 nil")
	}
}

func TestNewJWTManager_InvalidDuration(t *testing.T) {
	cases := []struct {
		name      string
		expires   string
		remember  string
		wantError bool
	}{
		{"valid_24h", "24h", "720h", false},
		{"valid_30d", "30d", "60d", false},
		{"valid_60m", "60m", "120m", false},
		{"invalid_expire", "abc", "720h", true},
		{"invalid_remember", "24h", "xyz", true},
		{"empty_expire", "", "720h", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewJWTManager(testSecret, tc.expires, tc.remember)
			if tc.wantError && err == nil {
				t.Error("期望错误但未返回")
			}
			if !tc.wantError && err != nil {
				t.Errorf("不期望错误但返回: %v", err)
			}
		})
	}
}

func TestJWTManager_GenerateAndVerify(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "24h", "720h")

	token, err := mgr.Generate(42, "user@example.com", "admin", false)
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if token == "" {
		t.Fatal("token 为空")
	}
	// JWT 应有三段
	if strings.Count(token, ".") != 2 {
		t.Errorf("token 格式错误，应含 2 个点: %q", token)
	}

	claims, err := mgr.Verify(token)
	if err != nil {
		t.Fatalf("Verify 失败: %v", err)
	}
	if claims.ID != 42 {
		t.Errorf("claims.ID 期望 42，实际 %d", claims.ID)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("claims.Email 期望 user@example.com，实际 %q", claims.Email)
	}
	if claims.Role != "admin" {
		t.Errorf("claims.Role 期望 admin，实际 %q", claims.Role)
	}
}

func TestJWTManager_RememberLongerThanNormal(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "1h", "720h")

	normalToken, _ := mgr.Generate(1, "a@b.com", "user", false)
	rememberToken, _ := mgr.Generate(1, "a@b.com", "user", true)

	normalClaims, _ := mgr.Verify(normalToken)
	rememberClaims, _ := mgr.Verify(rememberToken)

	normalExp := normalClaims.ExpiresAt.Time
	rememberExp := rememberClaims.ExpiresAt.Time

	if !rememberExp.After(normalExp) {
		t.Error("remember token 过期时间应晚于 normal token")
	}
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "1s", "1s")
	token, _ := mgr.Generate(1, "a@b.com", "user", false)

	time.Sleep(1100 * time.Millisecond)

	_, err := mgr.Verify(token)
	if err == nil {
		t.Fatal("过期 token 应校验失败")
	}
}

func TestJWTManager_TamperedToken(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "24h", "720h")
	token, _ := mgr.Generate(1, "a@b.com", "user", false)

	// 篡改 token 末尾
	tampered := token[:len(token)-2] + "XX"
	_, err := mgr.Verify(tampered)
	if err == nil {
		t.Fatal("篡改 token 应校验失败")
	}
}

func TestJWTManager_WrongSecret(t *testing.T) {
	mgr1, _ := NewJWTManager(testSecret, "24h", "720h")
	mgr2, _ := NewJWTManager("another-secret-key-different-from-test-32", "24h", "720h")

	token, _ := mgr1.Generate(1, "a@b.com", "user", false)
	_, err := mgr2.Verify(token)
	if err == nil {
		t.Fatal("不同密钥应校验失败")
	}
}

func TestJWTManager_KeyRotation(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "24h", "720h")

	// 用旧密钥生成 token
	oldToken, _ := mgr.Generate(1, "a@b.com", "user", false)

	// 轮换密钥
	mgr.rotator.Rotate("new-secret-key-after-rotation-32chars")

	// 旧 token 仍应能校验（旧密钥保留在历史列表）
	claims, err := mgr.Verify(oldToken)
	if err != nil {
		t.Fatalf("轮换后旧 token 应仍可校验: %v", err)
	}
	if claims.ID != 1 {
		t.Errorf("claims.ID 期望 1，实际 %d", claims.ID)
	}

	// 新 token 用新密钥签发
	newToken, _ := mgr.Generate(2, "b@b.com", "admin", false)
	_, err = mgr.Verify(newToken)
	if err != nil {
		t.Errorf("新 token 校验失败: %v", err)
	}
}

func TestJWTManager_RejectNonHS256(t *testing.T) {
	mgr, _ := NewJWTManager(testSecret, "24h", "720h")
	// 构造一个 None 算法 token（应被拒绝）
	// header: {"alg":"none","typ":"JWT"}
	noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJpZCI6MSwiZW1haWwiOiJhQGIuY29tIiwicm9sZSI6InVzZXIifQ."
	_, err := mgr.Verify(noneToken)
	if err == nil {
		t.Fatal("none 算法 token 应被拒绝")
	}
}

func TestKeyRotator_RotateKeepsHistory(t *testing.T) {
	r := NewKeyRotator("secret1")
	if len(r.OldKeys()) != 0 {
		t.Errorf("初始应无旧密钥，实际 %d", len(r.OldKeys()))
	}

	r.Rotate("secret2")
	if len(r.OldKeys()) != 1 {
		t.Errorf("一次轮换后应有 1 个旧密钥，实际 %d", len(r.OldKeys()))
	}
	if string(r.CurrentKey()) != "secret2" {
		t.Error("当前密钥应为 secret2")
	}

	r.Rotate("secret3")
	if len(r.OldKeys()) != 2 {
		t.Errorf("两次轮换后应有 2 个旧密钥，实际 %d", len(r.OldKeys()))
	}

	// 第三次轮换，应只保留最近 2 个
	r.Rotate("secret4")
	if len(r.OldKeys()) != 2 {
		t.Errorf("三次轮换后应保留 2 个旧密钥，实际 %d", len(r.OldKeys()))
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		input   string
		wantSec float64
		wantErr bool
	}{
		{"24h", 86400, false},
		{"30d", 30 * 86400, false},
		{"60m", 3600, false},
		{"3600s", 3600, false},
		{"1.5d", 1.5 * 86400, false},
		{"", 0, true},
		{"abc", 0, true},
		{"12", 0, true}, // 无单位
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			d, err := parseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("期望错误但未返回")
				}
				return
			}
			if err != nil {
				t.Errorf("不期望错误: %v", err)
				return
			}
			gotSec := d.Seconds()
			if gotSec != tc.wantSec {
				t.Errorf("parseDuration(%q) = %v 秒，期望 %v", tc.input, gotSec, tc.wantSec)
			}
		})
	}
}
