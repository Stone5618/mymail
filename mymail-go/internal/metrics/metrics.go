// Package metrics 定义 Prometheus 指标。
// 指标命名遵循 MyMail 命名空间约定：mymail_*。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP 指标
var (
	// HTTPRequestsTotal HTTP 请求总数
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_http_requests_total",
			Help: "HTTP 请求总数",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration HTTP 请求延迟分布
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mymail_http_request_duration_seconds",
			Help:    "HTTP 请求延迟（秒）",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// HTTPRequestsInFlight 当前在途请求数
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mymail_http_requests_in_flight",
			Help: "当前在途 HTTP 请求数",
		},
	)
)

// 业务指标（后续阶段使用）
var (
	// MailsReceivedTotal 接收邮件总数
	MailsReceivedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_mails_received_total",
			Help: "通过 SMTP 接收的邮件总数",
		},
		[]string{"folder", "spam_action"},
	)

	// MailsSentTotal 发送邮件总数
	MailsSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_mails_sent_total",
			Help: "发送邮件总数",
		},
		[]string{"status"},
	)

	// QueueDepth 发送队列当前深度
	QueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mymail_queue_depth",
			Help: "当前发送队列深度",
		},
	)
)

// SMTP 指标（阶段 4 使用）
var (
	// SMTPConnectionsActive 活跃 SMTP 连接数
	SMTPConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mymail_smtp_connections_active",
			Help: "活跃 SMTP 连接数",
		},
	)

	// SMTPConnectionsTotal SMTP 连接总数
	SMTPConnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_smtp_connections_total",
			Help: "SMTP 连接总数",
		},
		[]string{"result"},
	)
)

// 基础设施指标
var (
	// DBConnectionsActive 活跃数据库连接数
	DBConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mymail_db_connections_active",
			Help: "活跃数据库连接数",
		},
	)

	// AuthAttemptsTotal 认证尝试总数
	AuthAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_auth_attempts_total",
			Help: "认证尝试总数",
		},
		[]string{"method", "result"},
	)
)

// WebSocket 指标（阶段 6 使用）
var (
	// WSConnectionsActive 活跃 WebSocket 连接数
	WSConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "mymail_ws_connections_active",
			Help: "活跃 WebSocket 连接数",
		},
	)

	// WSConnectionsTotal WebSocket 连接总数
	WSConnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_ws_connections_total",
			Help: "WebSocket 连接总数",
		},
		[]string{"result"},
	)

	// WSMessagesSent WebSocket 发送消息总数
	WSMessagesSent = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mymail_ws_messages_sent_total",
			Help: "WebSocket 发送消息总数",
		},
		[]string{"type"},
	)
)
