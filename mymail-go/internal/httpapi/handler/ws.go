// ws.go 实现 WebSocket 升级 HTTP 端点。
//
// 端点：GET /ws（WebSocket 升级）
//
// 认证流程（P1-18：子协议优先 + URL query 兼容）：
//  1. 从 Sec-WebSocket-Protocol 头提取 auth.<token>（推荐）
//  2. 回退到 /ws?token=<token>（兼容）
//  3. JWT 验证 + 用户查找
//  4. 验证通过则接受升级，否则返回 401
//
// 生命周期：
//  1. 认证 → 接受 WS 升级 → 创建 Client → 注册 Hub → 发送欢迎消息 → 启动 WritePump
//  2. WritePump 阻塞直到连接关闭（心跳失败/客户端断开/服务器关闭）
//  3. 连接关闭后从 Hub 注销
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/metrics"
	"github.com/mymail/mymail-go/internal/storage/dao"
	wspkg "github.com/mymail/mymail-go/internal/ws"
)

// WSHandler 处理 WebSocket 升级请求。
type WSHandler struct {
	hub     *wspkg.Hub
	jwtMgr  *crypto.JWTManager
	userDAO *dao.UserDAO
}

// NewWSHandler 创建 WSHandler。
func NewWSHandler(hub *wspkg.Hub, jwtMgr *crypto.JWTManager, userDAO *dao.UserDAO) *WSHandler {
	return &WSHandler{
		hub:     hub,
		jwtMgr:  jwtMgr,
		userDAO: userDAO,
	}
}

// Upgrade 处理 GET /ws，升级为 WebSocket 连接。
//
// 响应：
//   - 401：认证失败（缺少/无效 token）
//   - 101：升级成功（WebSocket）
func (h *WSHandler) Upgrade(c *gin.Context) {
	// 1. 认证（P1-18：子协议优先 + URL query 兼容）
	userID, err := wspkg.Authenticate(c.Request, h.jwtMgr, h.userDAO)
	if err != nil {
		slog.Warn("WebSocket 认证失败", "error", err, "ip", c.ClientIP())
		metrics.WSConnectionsTotal.WithLabelValues("auth_failed").Inc()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "WebSocket 认证失败"})
		return
	}

	// 2. 接受 WebSocket 升级
	// 设置子协议回显（如果客户端通过子协议认证）
	token := wspkg.ExtractToken(c.Request)
	acceptOpts := &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	}
	if token != "" && c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
		// 回显子协议（与原 Node.js 行为一致）
		acceptOpts.Subprotocols = []string{wspkg.AuthTokenPrefix + token}
	}

	conn, err := websocket.Accept(c.Writer, c.Request, acceptOpts)
	if err != nil {
		slog.Error("WebSocket 升级失败", "error", err, "user_id", userID)
		metrics.WSConnectionsTotal.WithLabelValues("upgrade_failed").Inc()
		return
	}
	defer conn.Close(websocket.StatusInternalError, "内部错误")

	metrics.WSConnectionsTotal.WithLabelValues("success").Inc()

	// 3. 创建 Client 并注册到 Hub
	client := wspkg.NewClient(userID, conn, h.hub)
	h.hub.Register(userID, client)
	defer h.hub.Unregister(userID, client)

	slog.Info("WebSocket 连接建立", "user_id", userID, "ip", c.ClientIP())

	// 4. 发送欢迎消息（与原 Node.js 兼容）
	welcome := wspkg.Message{
		Type:    "connected",
		Message: "WebSocket 已连接",
	}
	welcomePayload, _ := json.Marshal(welcome)
	client.Send(welcomePayload)

	// 5. 启动 WritePump（阻塞直到连接关闭）
	// 使用 request context 确保连接随 HTTP 服务器关闭而关闭
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	client.WritePump(ctx)

	slog.Info("WebSocket 连接关闭", "user_id", userID)
}
