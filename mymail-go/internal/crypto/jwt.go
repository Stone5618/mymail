// Package crypto
// jwt.go 实现 JWT 生成与校验，支持 KeyRotator 密钥轮换。
//
// 兼容性：
//   - 算法：HS256（与 Node.js jsonwebtoken 默认一致）
//   - payload：{ id, email, role }（与原后端一致）
//   - 过期时间：支持 "24h"/"720h" 等字符串
//
// 企业级增强：
//   - KeyRotator：支持多密钥并存，校验时按顺序尝试，便于无缝轮换
//   - 旧密钥可设置保留期，到期后从轮换列表移除
package crypto

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 是 JWT payload 结构。
// 与原 Node.js 后端保持一致：仅 id / email / role 三字段 + 标准声明。
type Claims struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager 管理 JWT 生成与校验。
// 单密钥模式：直接用 NewJWTManager(secret, expiresIn)。
// 多密钥模式：用 KeyRotator 注入，支持轮换。
type JWTManager struct {
	rotator *KeyRotator
	expiresIn time.Duration
	rememberExpiresIn time.Duration
}

// NewJWTManager 创建单密钥 JWT 管理器。
// expiresIn / rememberExpiresIn 支持原 Node.js 的字符串格式（如 "24h", "30d"）。
func NewJWTManager(secret, expiresIn, rememberExpiresIn string) (*JWTManager, error) {
	exp, err := parseDuration(expiresIn)
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRES_IN 格式错误: %w", err)
	}
	remember, err := parseDuration(rememberExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("JWT_REMEMBER_EXPIRES_IN 格式错误: %w", err)
	}
	return &JWTManager{
		rotator:           NewKeyRotator(secret),
		expiresIn:         exp,
		rememberExpiresIn: remember,
	}, nil
}

// NewJWTManagerWithRotator 创建支持密钥轮换的 JWT 管理器。
func NewJWTManagerWithRotator(rotator *KeyRotator, expiresIn, rememberExpiresIn string) (*JWTManager, error) {
	exp, err := parseDuration(expiresIn)
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRES_IN 格式错误: %w", err)
	}
	remember, err := parseDuration(rememberExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("JWT_REMEMBER_EXPIRES_IN 格式错误: %w", err)
	}
	return &JWTManager{
		rotator:           rotator,
		expiresIn:         exp,
		rememberExpiresIn: remember,
	}, nil
}

// Generate 生成 JWT。
// remember=true 使用 rememberExpiresIn，否则使用 expiresIn。
func (m *JWTManager) Generate(id int64, email, role string, remember bool) (string, error) {
	now := time.Now()
	exp := m.expiresIn
	if remember {
		exp = m.rememberExpiresIn
	}
	claims := Claims{
		ID:    id,
		Email: email,
		Role:  role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(exp)),
		},
	}
	// 用当前主密钥签名
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.rotator.CurrentKey())
	if err != nil {
		return "", fmt.Errorf("签名 JWT 失败: %w", err)
	}
	return signed, nil
}

// Verify 校验 JWT，返回 Claims。
// 支持密钥轮换：依次尝试所有活跃密钥（含旧密钥），任一成功即可。
func (m *JWTManager) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		alg, ok := t.Method.(*jwt.SigningMethodHMAC)
		if !ok || alg.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("非预期的签名算法: %v", t.Header["alg"])
		}
		return m.rotator.CurrentKey(), nil
	})
	if err != nil {
		// 主密钥失败，尝试轮换中的旧密钥
		for _, key := range m.rotator.OldKeys() {
			_, err2 := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
				alg, ok := t.Method.(*jwt.SigningMethodHMAC)
				if !ok || alg.Alg() != jwt.SigningMethodHS256.Alg() {
					return nil, fmt.Errorf("非预期的签名算法")
				}
				return key, nil
			})
			if err2 == nil {
				return claims, nil
			}
		}
		return nil, fmt.Errorf("JWT 校验失败: %w", err)
	}
	return claims, nil
}

// KeyRotator 管理签名密钥轮换。
// 企业级能力：支持新旧密钥并存，新密钥签发，旧密钥仍可校验。
type KeyRotator struct {
	mu       sync.RWMutex
	current  []byte
	previous [][]byte
}

// NewKeyRotator 创建单密钥轮换器。
func NewKeyRotator(secret string) *KeyRotator {
	return &KeyRotator{
		current:  []byte(secret),
		previous: nil,
	}
}

// CurrentKey 返回当前签名密钥。
func (r *KeyRotator) CurrentKey() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// OldKeys 返回历史密钥列表（用于校验旧 token）。
func (r *KeyRotator) OldKeys() [][]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// 返回副本，避免外部修改
	out := make([][]byte, len(r.previous))
	copy(out, r.previous)
	return out
}

// Rotate 轮换密钥：当前密钥降级为历史密钥，新密钥成为当前密钥。
// 调用场景：定期轮换（如每月）、密钥泄露后紧急轮换。
func (r *KeyRotator) Rotate(newSecret string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.previous = append(r.previous, r.current)
	// 仅保留最近 2 个历史密钥（避免无限增长）
	if len(r.previous) > 2 {
		r.previous = r.previous[len(r.previous)-2:]
	}
	r.current = []byte(newSecret)
}

// parseDuration 解析时长字符串。
// 支持原 Node.js 的格式： "24h", "30d", "720h", "60m", "3600s"。
// "d" 后缀按天换算（与 js jsonwebtoken 一致）。
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("时长为空")
	}
	// 处理 "Nd" / "Ndays"
	if last := s[len(s)-1]; last == 'd' {
		var days float64
		_, err := fmt.Sscanf(s, "%fd", &days)
		if err != nil {
			return 0, fmt.Errorf("无法解析时长 %q: %w", s, err)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("无法解析时长 %q: %w", s, err)
	}
	return d, nil
}
