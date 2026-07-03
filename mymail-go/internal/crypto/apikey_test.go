// apikey_test.go 测试 API Key 生成与校验。
//
// 测试覆盖：
//   - GenerateAPIKey 格式（mk_ 前缀、67 字符、KeyHash 以 $2 开头、KeyPrefix 前 12 字符）
//   - GenerateAPIKey 唯一性（连续生成 10 个均不同）
//   - ExtractKeyPrefix（正常 key 提取前 12 字符、短字符串原样返回）
//   - VerifyAPIKey（正确明文通过、错误明文返回 error）
//   - IsAPIKeyFormat（mk_ 前缀+长度够返回 true、无前缀/太短返回 false）
package crypto

import (
	"strings"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	res, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey 失败: %v", err)
	}
	// mk_ 前缀
	if !strings.HasPrefix(res.PlainText, APIKeyPrefix) {
		t.Errorf("PlainText 应以 mk_ 开头，实际 %q", res.PlainText)
	}
	// 67 字符长度
	if len(res.PlainText) != 67 {
		t.Errorf("PlainText 长度应为 67，实际 %d", len(res.PlainText))
	}
	// KeyHash 非空且以 $2 开头
	if res.KeyHash == "" {
		t.Error("KeyHash 不应为空")
	}
	if !strings.HasPrefix(res.KeyHash, "$2") {
		t.Errorf("KeyHash 应以 $2 开头，实际 %q", res.KeyHash)
	}
	// KeyPrefix 为前 12 字符
	if res.KeyPrefix != res.PlainText[:12] {
		t.Errorf("KeyPrefix 应为前 12 字符，期望 %q，实际 %q", res.PlainText[:12], res.KeyPrefix)
	}
	// PlainText 与 KeyPrefix 前 12 字符匹配
	if res.PlainText[:12] != res.KeyPrefix {
		t.Errorf("PlainText 前 12 字符应与 KeyPrefix 匹配，PlainText[:12]=%q KeyPrefix=%q", res.PlainText[:12], res.KeyPrefix)
	}
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 10)
	for i := 0; i < 10; i++ {
		res, err := GenerateAPIKey()
		if err != nil {
			t.Fatalf("第 %d 次 GenerateAPIKey 失败: %v", i, err)
		}
		if seen[res.PlainText] {
			t.Errorf("第 %d 次生成的 key 重复: %q", i, res.PlainText)
		}
		seen[res.PlainText] = true
	}
	if len(seen) != 10 {
		t.Errorf("应生成 10 个不同 key，实际 %d", len(seen))
	}
}

func TestExtractKeyPrefix(t *testing.T) {
	// 正常 key 提取前 12 字符
	key := "mk_a1b2c3d4e5f6g7h8j9k0l1m2n3o4p5q6r7s8t9u0v1w2x3y4z5a6b7c8d9"
	prefix := ExtractKeyPrefix(key)
	if prefix != key[:12] {
		t.Errorf("前缀应为 %q，实际 %q", key[:12], prefix)
	}
	if len(prefix) != 12 {
		t.Errorf("前缀长度应为 12，实际 %d", len(prefix))
	}
	// 短字符串原样返回
	short := "mk_short"
	if got := ExtractKeyPrefix(short); got != short {
		t.Errorf("短字符串应原样返回，期望 %q，实际 %q", short, got)
	}
}

func TestVerifyAPIKey(t *testing.T) {
	res, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey 失败: %v", err)
	}
	// 正确明文验证通过
	if err := VerifyAPIKey(res.KeyHash, res.PlainText); err != nil {
		t.Errorf("正确明文验证应通过: %v", err)
	}
	// 错误明文返回 error
	if err := VerifyAPIKey(res.KeyHash, "mk_wrongkeyvalue"); err == nil {
		t.Error("错误明文应返回 error")
	}
}

func TestIsAPIKeyFormat(t *testing.T) {
	// mk_ 前缀+长度够返回 true
	if !IsAPIKeyFormat("mk_a1b2c3d4e5f6g7h8") {
		t.Error("mk_ 前缀且长度够应返回 true")
	}
	// 无 mk_ 前缀返回 false
	if IsAPIKeyFormat("xx_a1b2c3d4e5f6g7h8") {
		t.Error("无 mk_ 前缀应返回 false")
	}
	// 太短返回 false
	if IsAPIKeyFormat("mk_short") {
		t.Error("太短应返回 false")
	}
}
