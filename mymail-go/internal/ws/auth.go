// auth.go 实现 WebSocket 认证。
//
// P1-18 修复：WS token 走 URL query（支持子协议 + URL query 兼容）。
//
// 认证优先级：
//  1. Sec-WebSocket-Protocol: auth.<token>（推荐，避免 token 出现在 URL/日志）
//  2. /ws?token=<token>（兼容，用于无法设置子协议的客户端）
//
// 兼容性：与原 Node.js 子协议格式完全一致（auth. 前缀）。
package ws

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/storage/dao"
)

// AuthTokenPrefix 子协议认证前缀（与原 Node.js 一致）。
const AuthTokenPrefix = "auth."

// ErrMissingToken 缺少 token。
var ErrMissingToken = errors.New("缺少认证 token")

// ErrInvalidToken token 无效。
var ErrInvalidToken = errors.New("认证 token 无效")

// ExtractToken 从 HTTP 请求中提取 JWT token。
// P1-18：子协议优先 + URL query 兼容。
//
// 1. Sec-WebSocket-Protocol: auth.<token>（推荐）
// 2. /ws?token=<token>（兼容）
func ExtractToken(r *http.Request) string {
	// 1. 子协议优先（推荐方式，token 不出现在 URL/日志中）
	protocols := r.Header.Get("Sec-WebSocket-Protocol")
	if protocols != "" {
		for _, p := range strings.Split(protocols, ",") {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, AuthTokenPrefix) {
				return p[len(AuthTokenPrefix):]
			}
		}
	}

	// 2. URL query 兼容（用于无法设置子协议的客户端）
	return r.URL.Query().Get("token")
}

// Authenticate 验证 JWT token 并返回用户 ID。
//
// 校验流程：
//  1. 提取 token（子协议优先 + URL query 兼容）
//  2. JWT 验证
//  3. 用户存在性检查
//  4. 用户活跃状态检查
func Authenticate(r *http.Request, jwtMgr *crypto.JWTManager, userDAO *dao.UserDAO) (int64, error) {
	token := ExtractToken(r)
	if token == "" {
		return 0, ErrMissingToken
	}

	claims, err := jwtMgr.Verify(token)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	user, err := userDAO.FindByID(r.Context(), claims.ID)
	if err != nil {
		return 0, fmt.Errorf("查询用户失败: %w", err)
	}
	if user == nil {
		return 0, errors.New("用户不存在")
	}
	if !user.IsActive {
		return 0, errors.New("用户已被禁用")
	}

	return user.ID, nil
}
