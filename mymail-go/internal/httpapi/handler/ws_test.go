// ws_test.go 测试 WebSocket 升级端点。
//
// 验收标准覆盖：
//   - URL query 兼容（P1-18）
//   - 子协议认证可用（P1-18）
//   - 新邮件到达时客户端收到通知
//   - 未认证连接被拒绝（401）
//
// 测试使用 httptest.Server + coder/websocket.Dial 进行端到端集成测试。
package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/mymail/mymail-go/internal/crypto"
	wspkg "github.com/mymail/mymail-go/internal/ws"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// wsTestEnv WebSocket 测试环境。
type wsTestEnv struct {
	hub     *wspkg.Hub
	jwtMgr  *crypto.JWTManager
	userDAO *dao.UserDAO
	userID  int64
	token   string
	srv     *httptest.Server
}

// setupWSTestEnv 创建测试环境（含真实 DB + JWT + Hub + HTTP 服务器）。
func setupWSTestEnv(t *testing.T) *wsTestEnv {
	t.Helper()

	// 1. DB + 用户
	path := filepath.Join(t.TempDir(), "ws_handler_test.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	userDAO := dao.NewUserDAO(database)
	uid, err := userDAO.Create(context.Background(), dao.CreateUserInput{
		Username: "wstest", Email: "wstest@example.com", PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}

	// 2. JWT
	jwtMgr, err := crypto.NewJWTManager("ws-test-secret-123456", "24h", "720h")
	if err != nil {
		t.Fatalf("创建 JWT 失败: %v", err)
	}
	token, err := jwtMgr.Generate(uid, "wstest@example.com", "user", false)
	if err != nil {
		t.Fatalf("生成 token 失败: %v", err)
	}

	// 3. Hub + Handler
	hub := wspkg.NewHub()
	wsH := NewWSHandler(hub, jwtMgr, userDAO)

	// 4. Gin + httptest.Server
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", wsH.Upgrade)
	srv := httptest.NewServer(r)
	t.Cleanup(func() {
		srv.Close()
		hub.Shutdown()
	})

	return &wsTestEnv{
		hub:     hub,
		jwtMgr:  jwtMgr,
		userDAO: userDAO,
		userID:  uid,
		token:   token,
		srv:     srv,
	}
}

// wsURL 将 http:// URL 转为 ws:// URL。
func wsURL(httpURL, path string) string {
	return strings.Replace(httpURL+path, "http://", "ws://", 1)
}

func TestWSHandler_Upgrade_MissingToken(t *testing.T) {
	env := setupWSTestEnv(t)

	// 无 token 连接应返回 401
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"), nil)
	if err == nil {
		t.Error("无 token 应连接失败")
	}
}

func TestWSHandler_Upgrade_InvalidToken(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"),
		&websocket.DialOptions{
			Subprotocols: []string{"auth.invalid.token"},
		})
	if err == nil {
		t.Error("无效 token 应连接失败")
	}
}

func TestWSHandler_Upgrade_SubprotocolAuth(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 通过子协议认证
	conn, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"),
		&websocket.DialOptions{
			Subprotocols: []string{"auth." + env.token},
		})
	if err != nil {
		t.Fatalf("子协议认证连接失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	// 读取欢迎消息
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()

	_, payload, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("读取欢迎消息失败: %v", err)
	}

	var msg wspkg.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("解析欢迎消息失败: %v", err)
	}
	if msg.Type != "connected" {
		t.Errorf("欢迎消息 Type 期望 'connected'，实际 %q", msg.Type)
	}
	if msg.Message != "WebSocket 已连接" {
		t.Errorf("欢迎消息 Message 期望 'WebSocket 已连接'，实际 %q", msg.Message)
	}
}

func TestWSHandler_Upgrade_URLQueryAuth(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 通过 URL query 认证（P1-18 兼容）
	conn, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws?token="+env.token), nil)
	if err != nil {
		t.Fatalf("URL query 认证连接失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	// 读取欢迎消息
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()

	_, payload, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("读取欢迎消息失败: %v", err)
	}

	var msg wspkg.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("解析欢迎消息失败: %v", err)
	}
	if msg.Type != "connected" {
		t.Errorf("欢迎消息 Type 期望 'connected'，实际 %q", msg.Type)
	}
}

func TestWSHandler_NewMailNotification(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 建立连接
	conn, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"),
		&websocket.DialOptions{
			Subprotocols: []string{"auth." + env.token},
		})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	// 读取欢迎消息
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, _, _ = conn.Read(readCtx)

	// 等待连接注册到 Hub
	time.Sleep(100 * time.Millisecond)

	// 通过 Hub 发送新邮件通知
	mailData := wspkg.NewMailData{
		ID: 100, From: "sender@test.com", Subject: "测试新邮件", Time: "2026-07-04 15:00:00",
	}
	env.hub.SendNewMail(env.userID, mailData)

	// 读取新邮件通知
	readCtx2, readCancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel2()

	_, payload, err := conn.Read(readCtx2)
	if err != nil {
		t.Fatalf("读取新邮件通知失败: %v", err)
	}

	var msg wspkg.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("解析新邮件通知失败: %v", err)
	}
	if msg.Type != "new_mail" {
		t.Errorf("Type 期望 'new_mail'，实际 %q", msg.Type)
	}

	// 验证邮件数据
	dataBytes, _ := json.Marshal(msg.Data)
	var gotData wspkg.NewMailData
	if err := json.Unmarshal(dataBytes, &gotData); err != nil {
		t.Fatalf("解析邮件数据失败: %v", err)
	}
	if gotData.ID != 100 {
		t.Errorf("邮件 ID 期望 100，实际 %d", gotData.ID)
	}
	if gotData.From != "sender@test.com" {
		t.Errorf("From 期望 'sender@test.com'，实际 %q", gotData.From)
	}
	if gotData.Subject != "测试新邮件" {
		t.Errorf("Subject 期望 '测试新邮件'，实际 %q", gotData.Subject)
	}
}

func TestWSHandler_QueueCompleteNotification(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"),
		&websocket.DialOptions{
			Subprotocols: []string{"auth." + env.token},
		})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	// 读取欢迎消息
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, _, _ = conn.Read(readCtx)

	time.Sleep(100 * time.Millisecond)

	// 发送队列完成通知
	env.hub.SendQueueComplete(env.userID, map[string]interface{}{"mail_id": 42})

	// 读取通知
	readCtx2, readCancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel2()

	_, payload, err := conn.Read(readCtx2)
	if err != nil {
		t.Fatalf("读取队列完成通知失败: %v", err)
	}

	var msg wspkg.Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("解析队列完成通知失败: %v", err)
	}
	if msg.Type != "queue_complete" {
		t.Errorf("Type 期望 'queue_complete'，实际 %q", msg.Type)
	}
}

func TestWSHandler_ConnectionUnregisterOnClose(t *testing.T) {
	env := setupWSTestEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(env.srv.URL, "/ws"),
		&websocket.DialOptions{
			Subprotocols: []string{"auth." + env.token},
		})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	// 读取欢迎消息
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, _, _ = conn.Read(readCtx)

	time.Sleep(100 * time.Millisecond)

	// 验证连接已注册
	if env.hub.ClientCount() != 1 {
		t.Errorf("连接后 ClientCount 期望 1，实际 %d", env.hub.ClientCount())
	}

	// 关闭连接
	conn.Close(websocket.StatusNormalClosure, "test done")
	time.Sleep(200 * time.Millisecond)

	// 验证连接已注销
	if env.hub.ClientCount() != 0 {
		t.Errorf("关闭后 ClientCount 期望 0，实际 %d", env.hub.ClientCount())
	}
}
