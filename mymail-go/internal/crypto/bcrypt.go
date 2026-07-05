// Package crypto 提供密码哈希与 JWT 工具。
package crypto

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost 是 bcrypt 哈希成本因子。
// 与原 Node.js 后端保持一致（cost=12），确保密码哈希互通。
const BcryptCost = 12

// HashPassword 对明文密码进行 bcrypt 哈希。
// 生成的 hash 以 $2a$ 开头（Go 实现），可被 Node.js bcrypt.compare 验证。
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("密码哈希失败: %w", err)
	}
	return string(hash), nil
}

// ComparePassword 校验明文密码与哈希是否匹配。
// 兼容 $2a$ / $2b$ / $2y$ 三种前缀（Go bcrypt.CompareHashAndPassword 原生支持）。
// 返回 nil 表示匹配，返回 error 表示不匹配或哈希格式错误。
func ComparePassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// HashDovecotPassword 生成 Dovecot 兼容的 BLF-CRYPT 哈希。
//
// Dovecot 2.3+ 的 BLF-CRYPT 实现原生支持 $2a$ / $2b$ / $2y$ 三种 bcrypt 前缀
//（参见 Dovecot Password Schemes 文档）。Go 的 golang.org/x/crypto/bcrypt 生成
// 的 $2a$ 前缀哈希可直接用于 Dovecot IMAP 认证，无需额外调用 doveadm。
func HashDovecotPassword(password string) (string, error) {
	return HashPassword(password)
}
