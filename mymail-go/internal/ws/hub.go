// Package ws 实现 WebSocket 实时推送，支持新邮件通知与队列完成通知。
//
// 设计：
//   - Hub：用户维度连接管理（userID → []*Client），线程安全
//   - Client：单连接处理，心跳 30s ping/pong
//   - Auth：P1-18 子协议优先 + URL query 兼容
//
// 消息格式（与原 Node.js 兼容）：
//
//	{ "type": "connected", "message": "WebSocket 已连接" }
//	{ "type": "new_mail", "data": { "id": 1, "from": "...", "subject": "...", "time": "..." } }
//	{ "type": "queue_complete", "data": { ... } }
package ws

import (
	"encoding/json"
	"sync"

	"github.com/mymail/mymail-go/internal/metrics"
)

// NewMailData 新邮件通知数据。
type NewMailData struct {
	ID      int64  `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Time    string `json:"time"`
}

// Message WebSocket 消息（与原 Node.js 格式兼容）。
type Message struct {
	Type    string      `json:"type"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

// Hub 管理所有 WebSocket 连接，按 userID 分组。
// 线程安全：所有方法均可并发调用。
type Hub struct {
	mu      sync.RWMutex
	clients map[int64]map[*Client]bool // userID → set of clients
}

// NewHub 创建 Hub。
func NewHub() *Hub {
	return &Hub{
		clients: make(map[int64]map[*Client]bool),
	}
}

// Register 注册客户端到 Hub。
func (h *Hub) Register(userID int64, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*Client]bool)
	}
	h.clients[userID][c] = true
	metrics.WSConnectionsActive.Inc()
}

// Unregister 从 Hub 注销客户端。
func (h *Hub) Unregister(userID int64, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.clients[userID]; ok {
		if _, exists := clients[c]; exists {
			delete(clients, c)
			metrics.WSConnectionsActive.Dec()
		}
		if len(clients) == 0 {
			delete(h.clients, userID)
		}
	}
}

// SendToUser 向指定用户的所有连接发送原始消息。
func (h *Hub) SendToUser(userID int64, payload []byte) {
	h.mu.RLock()
	clients := h.clients[userID]
	snapshot := make([]*Client, 0, len(clients))
	for c := range clients {
		snapshot = append(snapshot, c)
	}
	h.mu.RUnlock()

	for _, c := range snapshot {
		c.Send(payload)
	}
}

// SendNewMail 向指定用户发送新邮件通知。
func (h *Hub) SendNewMail(userID int64, data NewMailData) {
	msg := Message{Type: "new_mail", Data: data}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	metrics.WSMessagesSent.WithLabelValues("new_mail").Inc()
	h.SendToUser(userID, payload)
}

// SendQueueComplete 向指定用户发送队列完成通知。
func (h *Hub) SendQueueComplete(userID int64, data interface{}) {
	msg := Message{Type: "queue_complete", Data: data}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	metrics.WSMessagesSent.WithLabelValues("queue_complete").Inc()
	h.SendToUser(userID, payload)
}

// Shutdown 关闭所有连接（优雅关闭，draining）。
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for userID, clients := range h.clients {
		for c := range clients {
			c.Close()
		}
		delete(h.clients, userID)
	}
}

// ClientCount 返回总连接数。
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, clients := range h.clients {
		count += len(clients)
	}
	return count
}

// UserCount 返回在线用户数。
func (h *Hub) UserCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
