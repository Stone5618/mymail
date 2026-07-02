// Package handler 提供 HTTP 请求处理器。
package handler

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// HealthHandler 处理健康检查端点。
// 实现 P1-8 修复：liveness/readiness/startup 三端点分离。
type HealthHandler struct {
	db      *db.DB
	started atomic.Bool
}

// NewHealthHandler 创建健康检查处理器。
func NewHealthHandler(database *db.DB) *HealthHandler {
	h := &HealthHandler{db: database}
	// 启动后标记为已启动（延迟 2 秒，模拟预热完成）
	go func() {
		time.Sleep(2 * time.Second)
		h.started.Store(true)
	}()
	return h
}

// Liveness 存活探针：进程是否存活。
// 用于 Kubernetes livenessProbe，失败则重启容器。
// GET /healthz
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "alive",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Readiness 就绪探针：是否准备好接收流量。
// 用于 Kubernetes readinessProbe，失败则从负载均衡移除。
// 检查 DB 连通性。
// GET /readyz
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	checks := gin.H{}
	allOK := true

	// 检查启动状态
	if !h.started.Load() {
		checks["startup"] = "starting"
		allOK = false
	} else {
		checks["startup"] = "ready"
	}

	// 检查数据库连通性
	if h.db != nil {
		if err := h.db.PingContext(ctx); err != nil {
			checks["db"] = "down"
			allOK = false
		} else {
			checks["db"] = "up"
		}
	} else {
		checks["db"] = "not_configured"
		allOK = false
	}

	if !allOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": checks,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
		"checks": checks,
	})
}

// Startup 启动探针：应用是否已启动完成。
// 用于 Kubernetes startupProbe，避免慢启动应用被 liveness 杀死。
// GET /startupz
func (h *HealthHandler) Startup(c *gin.Context) {
	if !h.started.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "starting"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "started"})
}
