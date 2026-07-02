// Package httpapi 提供 HTTP API 路由与中间件注册。
package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// NewRouter 构建 Gin 路由，注册全部中间件与健康检查端点。
// 阶段 1 仅包含基础设施端点，业务端点在后续阶段添加。
func NewRouter(cfg *config.Config, database *db.DB) *gin.Engine {
	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// ===== 全局中间件链 =====
	// 注意顺序：Recover 必须最先，Security 在最外层
	r.Use(middleware.Recover())
	r.Use(middleware.Security())
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORS(cfg)) // P1-2 修复：白名单而非全开放
	r.Use(middleware.Metrics())

	// ===== 基础设施端点（无需认证） =====
	healthH := handler.NewHealthHandler(database)
	r.GET("/healthz", healthH.Liveness)
	r.GET("/readyz", healthH.Readiness)
	r.GET("/startupz", healthH.Startup)

	// Prometheus 指标端点
	if cfg.MetricsEnabled {
		r.GET(cfg.MetricsPath, gin.WrapH(promhttp.Handler()))
	}

	// ===== 业务端点（后续阶段添加） =====
	// /api/auth/*     —— 阶段 2
	// /api/mail/*     —— 阶段 3
	// /api/admin/*    —— 阶段 5
	// /api/v1/*       —— 阶段 5
	// /api/rules/*    —— 阶段 5
	// /ws             —— 阶段 6

	// 健康检查别名（兼容旧客户端）
	r.GET("/health", healthH.Liveness)

	return r
}
