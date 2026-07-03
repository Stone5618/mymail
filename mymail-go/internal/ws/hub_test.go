// hub_test.go 测试 Hub 的连接管理、消息分发与优雅关闭。
//
// 测试覆盖：
//   - Register/Unregister（ClientCount/UserCount 变化）
//   - SendToUser（定向发送）
//   - SendNewMail（新邮件通知格式）
//   - SendQueueComplete（队列完成通知格式）
//   - Shutdown（关闭所有连接）
//   - 并发安全（多 goroutine 同时 Register/Send）
package ws

import (
	"encoding/json"
	"sync"
	"testing"
)

// newTestClient 创建测试用 Client（无真实 WS 连接，仅用于 Hub 测试）。
func newTestClient(userID int64) *Client {
	return &Client{
		userID: userID,
		send:   make(chan []byte, sendBufferSize),
		done:   make(chan struct{}),
	}
}

func TestHub_NewHub(t *testing.T) {
	h := NewHub()
	if h == nil {
		t.Fatal("NewHub 返回 nil")
	}
	if h.ClientCount() != 0 {
		t.Errorf("新 Hub ClientCount 期望 0，实际 %d", h.ClientCount())
	}
	if h.UserCount() != 0 {
		t.Errorf("新 Hub UserCount 期望 0，实际 %d", h.UserCount())
	}
}

func TestHub_RegisterUnregister(t *testing.T) {
	h := NewHub()
	c1 := newTestClient(1)
	c2 := newTestClient(1) // 同一用户两个连接
	c3 := newTestClient(2) // 另一用户

	h.Register(1, c1)
	h.Register(1, c2)
	h.Register(2, c3)

	if h.ClientCount() != 3 {
		t.Errorf("注册 3 个连接后 ClientCount 期望 3，实际 %d", h.ClientCount())
	}
	if h.UserCount() != 2 {
		t.Errorf("注册 2 个用户后 UserCount 期望 2，实际 %d", h.UserCount())
	}

	// 注销 c1
	h.Unregister(1, c1)
	if h.ClientCount() != 2 {
		t.Errorf("注销后 ClientCount 期望 2，实际 %d", h.ClientCount())
	}
	if h.UserCount() != 2 {
		t.Errorf("用户 1 仍有连接，UserCount 期望 2，实际 %d", h.UserCount())
	}

	// 注销 c2（用户 1 的最后一个连接）
	h.Unregister(1, c2)
	if h.ClientCount() != 1 {
		t.Errorf("注销后 ClientCount 期望 1，实际 %d", h.ClientCount())
	}
	if h.UserCount() != 1 {
		t.Errorf("用户 1 无连接后 UserCount 期望 1，实际 %d", h.UserCount())
	}

	// 重复注销（幂等）
	h.Unregister(1, c1)
	if h.ClientCount() != 1 {
		t.Errorf("重复注销不应影响计数，ClientCount 期望 1，实际 %d", h.ClientCount())
	}
}

func TestHub_SendToUser(t *testing.T) {
	h := NewHub()
	c1 := newTestClient(1)
	c2 := newTestClient(1)
	c3 := newTestClient(2)

	h.Register(1, c1)
	h.Register(1, c2)
	h.Register(2, c3)

	// 向用户 1 发送消息
	msg := []byte(`{"type":"test"}`)
	h.SendToUser(1, msg)

	// c1 和 c2 应收到消息
	select {
	case got := <-c1.send:
		if string(got) != `{"type":"test"}` {
			t.Errorf("c1 收到错误消息: %s", got)
		}
	default:
		t.Error("c1 未收到消息")
	}

	select {
	case got := <-c2.send:
		if string(got) != `{"type":"test"}` {
			t.Errorf("c2 收到错误消息: %s", got)
		}
	default:
		t.Error("c2 未收到消息")
	}

	// c3 不应收到消息
	select {
	case <-c3.send:
		t.Error("c3 不应收到用户 1 的消息")
	default:
	}
}

func TestHub_SendNewMail(t *testing.T) {
	h := NewHub()
	c := newTestClient(1)
	h.Register(1, c)

	data := NewMailData{
		ID: 42, From: "sender@example.com", Subject: "测试邮件", Time: "2026-07-04 12:00:00",
	}
	h.SendNewMail(1, data)

	select {
	case payload := <-c.send:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("解析消息失败: %v", err)
		}
		if msg.Type != "new_mail" {
			t.Errorf("Type 期望 new_mail，实际 %s", msg.Type)
		}
		// 验证 data 字段
		dataBytes, _ := json.Marshal(msg.Data)
		var mailData NewMailData
		if err := json.Unmarshal(dataBytes, &mailData); err != nil {
			t.Fatalf("解析 data 失败: %v", err)
		}
		if mailData.ID != 42 {
			t.Errorf("邮件 ID 期望 42，实际 %d", mailData.ID)
		}
		if mailData.From != "sender@example.com" {
			t.Errorf("From 不匹配: %s", mailData.From)
		}
		if mailData.Subject != "测试邮件" {
			t.Errorf("Subject 不匹配: %s", mailData.Subject)
		}
	default:
		t.Error("未收到新邮件通知")
	}
}

func TestHub_SendQueueComplete(t *testing.T) {
	h := NewHub()
	c := newTestClient(1)
	h.Register(1, c)

	h.SendQueueComplete(1, map[string]interface{}{"mail_id": 99})

	select {
	case payload := <-c.send:
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("解析消息失败: %v", err)
		}
		if msg.Type != "queue_complete" {
			t.Errorf("Type 期望 queue_complete，实际 %s", msg.Type)
		}
	default:
		t.Error("未收到队列完成通知")
	}
}

func TestHub_SendToUser_NoClients(t *testing.T) {
	h := NewHub()
	// 向无连接的用户发送不应 panic
	h.SendToUser(999, []byte("test"))
	h.SendNewMail(999, NewMailData{})
	h.SendQueueComplete(999, nil)
}

func TestHub_Shutdown(t *testing.T) {
	h := NewHub()
	c1 := newTestClient(1)
	c2 := newTestClient(2)
	c3 := newTestClient(3)

	h.Register(1, c1)
	h.Register(2, c2)
	h.Register(3, c3)

	h.Shutdown()

	if h.ClientCount() != 0 {
		t.Errorf("Shutdown 后 ClientCount 期望 0，实际 %d", h.ClientCount())
	}
	if h.UserCount() != 0 {
		t.Errorf("Shutdown 后 UserCount 期望 0，实际 %d", h.UserCount())
	}

	// 验证所有 client 的 done channel 已关闭
	for _, c := range []*Client{c1, c2, c3} {
		select {
		case <-c.done:
			// 正确：done 已关闭
		default:
			t.Error("Client 的 done channel 未关闭")
		}
	}
}

func TestHub_ConcurrentSafe(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup

	// 10 个 goroutine 同时注册/注销/发送
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := newTestClient(int64(idx))
			h.Register(int64(idx), c)
			h.SendToUser(int64(idx), []byte("test"))
			h.SendNewMail(int64(idx), NewMailData{ID: int64(idx)})
			h.Unregister(int64(idx), c)
		}(i)
	}
	wg.Wait()

	if h.ClientCount() != 0 {
		t.Errorf("并发操作后 ClientCount 期望 0，实际 %d", h.ClientCount())
	}
}

func TestHub_MultipleClientsSameUser(t *testing.T) {
	h := NewHub()
	// 同一用户 5 个连接
	clients := make([]*Client, 5)
	for i := range clients {
		clients[i] = newTestClient(100)
		h.Register(100, clients[i])
	}

	if h.UserCount() != 1 {
		t.Errorf("同一用户 5 连接 UserCount 期望 1，实际 %d", h.UserCount())
	}
	if h.ClientCount() != 5 {
		t.Errorf("ClientCount 期望 5，实际 %d", h.ClientCount())
	}

	// 发送消息，所有 5 个连接都应收到
	h.SendToUser(100, []byte("broadcast"))

	for i, c := range clients {
		select {
		case <-c.send:
			// 正确
		default:
			t.Errorf("client %d 未收到消息", i)
		}
	}

	// 注销一个，其余仍应收到
	h.Unregister(100, clients[0])
	h.SendToUser(100, []byte("second"))

	for i := 1; i < 5; i++ {
		select {
		case <-clients[i].send:
			// 正确
		default:
			t.Errorf("client %d 未收到第二条消息", i)
		}
	}
}
