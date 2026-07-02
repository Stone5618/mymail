// Package httpapi 提供 HTTP API 路由与中间件注册。
package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
)

// Deps 聚合路由注册所需依赖。
type Deps struct {
	Cfg         *config.Config
	DB          *db.DB
	UserDAO     *dao.UserDAO
	JWTManager  *crypto.JWTManager
	AuthService *service.AuthService
}

// NewRouter 构建 Gin 路由，注册全部中间件与端点。
func NewRouter(deps Deps) *gin.Engine {
	if deps.Cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// ===== 全局中间件链 =====
	r.Use(middleware.Recover())
	r.Use(middleware.Security())
	r.Use(middleware.RequestID())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.CORS(deps.Cfg))
	r.Use(middleware.Metrics())

	// ===== 基础设施端点（无需认证） =====
	healthH := handler.NewHealthHandler(deps.DB)
	r.GET("/healthz", healthH.Liveness)
	r.GET("/readyz", healthH.Readiness)
	r.GET("/startupz", healthH.Startup)
	if deps.Cfg.MetricsEnabled {
		r.GET(deps.Cfg.MetricsPath, gin.WrapH(promhttp.Handler()))
	}
	r.GET("/health", healthH.Liveness) // 兼容别名

	// ===== 业务端点 =====
	registerAuthRoutes(r, deps)

	// 后续阶段添加：
	// /api/mail/*  —— 阶段 3
	// /api/admin/* —— 阶段 5
	// /api/v1/*    —— 阶段 5
	// /api/rules/* —— 阶段 5
	// /ws          —— 阶段 6

	return r
}

// registerAuthRoutes 注册 /api/auth/* 路由。
func registerAuthRoutes(r *gin.Engine, deps Deps) {
	authH := handler.NewAuthHandler(deps.AuthService)

	auth := r.Group("/api/auth")
	{
		// 公开端点（无需认证）
		auth.POST("/register", authH.Register)
		auth.POST("/login", authH.Login)

		// 需认证端点
		secured := auth.Group("")
		secured.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
		{
			secured.GET("/me", authH.Me)
			secured.PUT("/profile", authH.UpdateProfile)
			secured.PUT("/password", authH.ChangePassword)
			secured.POST("/change-default-password", authH.ChangeDefaultPassword)
		}
	}
}
