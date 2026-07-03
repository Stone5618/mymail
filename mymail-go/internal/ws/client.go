// client.go 实现单个 WebSocket 连接的处理。
//
// 企业级能力：
//   - 心跳 30s ping/pong（检测客户端存活）
//   - 消息大小限制（maxPayload 1MB）
//   - 优雅关闭（通过 done channel 触发）
//   - 指标埋点（ws_connections_active 由 Hub 管理）
//
// 生命周期：
//  1. NewClient 创建客户端，设置读限制
//  2. WritePump 在独立 goroutine 中运行，处理写消息 + 心跳
//  3. Send 非阻塞发送消息
//  4. Close 优雅关闭
package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// sendBufferSize 发送通道缓冲大小。
	// 超过此大小的消息会被丢弃（客户端处理不过来时）。
	sendBufferSize = 64

	// MaxPayloadSize 最大消息大小（1MB）。
	MaxPayloadSize = 1 << 20

	// HeartbeatInterval 心跳间隔（30s ping/pong）。
	HeartbeatInterval = 30 * time.Second

	// pingTimeout Ping 超时时间（10s 内未收到 pong 则断开）。
	pingTimeout = 10 * time.Second

	// writeTimeout 写操作超时。
	writeTimeout = 10 * time.Second
)

// Client 封装单个 WebSocket 连接。
type Client struct {
	userID    int64
	conn      *websocket.Conn
	hub       *Hub
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// NewClient 创建客户端。
func NewClient(userID int64, conn *websocket.Conn, hub *Hub) *Client {
	conn.SetReadLimit(MaxPayloadSize)
	return &Client{
		userID: userID,
		conn:   conn,
		hub:    hub,
		send:   make(chan []byte, sendBufferSize),
		done:   make(chan struct{}),
	}
}

// UserID 返回客户端关联的用户 ID。
func (c *Client) UserID() int64 {
	return c.userID
}

// WritePump 处理写消息 + 心跳。
// 应在独立 goroutine 中运行，阻塞直到连接关闭。
//
// 退出条件（任一满足即返回，触发 handler defer Unregister）：
//  1. send channel 关闭
//  2. 写入失败（客户端已断开）
//  3. ping 超时（客户端无响应）
//  4. done channel 触发（服务器主动关闭）
//  5. ctx.Done()（HTTP 服务器关闭）
//  6. readCtx.Done()（客户端主动关闭连接，由 CloseRead 监听）
func (c *Client) WritePump(ctx context.Context) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	// CloseRead 将读端关闭：返回的 ctx 在客户端断开（正常关闭/异常断开）时取消。
	// 这是检测客户端主动关闭的标准方式（比等待 30s ping 失败更快）。
	readCtx := c.conn.CloseRead(ctx)

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// send channel 已关闭
				c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			// 心跳：发送 Ping，超时则断开
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				// Ping 失败（客户端未响应 pong 或连接已断开）
				return
			}
		case <-c.done:
			c.conn.Close(websocket.StatusGoingAway, "服务器关闭")
			return
		case <-ctx.Done():
			c.conn.Close(websocket.StatusGoingAway, "")
			return
		case <-readCtx.Done():
			// 客户端已关闭连接（正常关闭或异常断开）
			return
		}
	}
}

// Send 非阻塞发送消息到客户端。
// 如果发送缓冲区已满（客户端处理不过来），消息会被丢弃。
func (c *Client) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
		// 缓冲区已满，丢弃消息
	}
}

// Close 优雅关闭客户端连接。
// 可安全多次调用（sync.Once 保证只关闭一次）。
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}
