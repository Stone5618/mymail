// apikey.go 实现 API Key 生成与校验。
//
// 设计（P1-3 修复）：
//   - 格式：mk_<32字节hex>（共 67 字符）
//   - key_prefix：前 12 字符（mk_ + 9 hex），存入 DB 用于 O(1) 索引查找
//   - 哈希：bcrypt（复用 HashPassword），与用户密码哈希互通
//   - 查找流程：明文 key → 提取 prefix → WHERE key_prefix=? → bcrypt.Compare
//     避免原 Node.js 全表 O(n) bcrypt 比对
package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// APIKeyPrefix 是 API Key 的固定前缀。
	APIKeyPrefix = "mk_"
	// keyPrefixLength 是用于索引的 key_prefix 长度（mk_ + 9 hex = 12 字符）。
	keyPrefixLength = 12
	// keyRandomBytes 是随机部分的字节数（32 字节 = 64 hex 字符）。
	keyRandomBytes = 32
)

// APIKeyResult 生成 API Key 的结果。
//
// PlainText 仅在创建时返回一次，后续无法恢复。
// KeyHash 存入数据库。
// KeyPrefix 存入数据库用于索引查找。
type APIKeyResult struct {
	PlainText string // 明文 key（mk_ + 64 hex），仅创建时返回一次
	KeyHash   string // bcrypt 哈希
	KeyPrefix string // 前缀（12 字符），用于索引查找
}

// GenerateAPIKey 生成新的 API Key（明文 + 哈希 + 前缀）。
func GenerateAPIKey() (*APIKeyResult, error) {
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("生成随机数失败: %w", err)
	}
	plaintext := APIKeyPrefix + hex.EncodeToString(buf)

	hash, err := HashPassword(plaintext)
	if err != nil {
		return nil, fmt.Errorf("哈希 API Key 失败: %w", err)
	}

	return &APIKeyResult{
		PlainText: plaintext,
		KeyHash:   hash,
		KeyPrefix: ExtractKeyPrefix(plaintext),
	}, nil
}

// ExtractKeyPrefix 从明文 key 提取前缀（前 12 字符）。
// 用于 key_prefix 索引查找（P1-3 修复）。
func ExtractKeyPrefix(key string) string {
	if len(key) < keyPrefixLength {
		return key
	}
	return key[:keyPrefixLength]
}

// VerifyAPIKey 校验明文 key 与哈希是否匹配。
// 复用 bcrypt.ComparePassword。
func VerifyAPIKey(hash, plaintext string) error {
	return ComparePassword(hash, plaintext)
}

// IsAPIKeyFormat 检查字符串是否符合 API Key 格式（mk_ 前缀 + 至少 12 字符）。
func IsAPIKeyFormat(s string) bool {
	return strings.HasPrefix(s, APIKeyPrefix) && len(s) >= keyPrefixLength
}
