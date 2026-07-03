// Package httpapi 提供 HTTP API 路由与中间件注册。
package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/crypto"
	"github.com/mymail/mymail-go/internal/httpapi/handler"
	"github.com/mymail/mymail-go/internal/httpapi/middleware"
	"github.com/mymail/mymail-go/internal/service"
	"github.com/mymail/mymail-go/internal/storage/attachment"
	"github.com/mymail/mymail-go/internal/storage/dao"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/ws"
	"github.com/mymail/mymail-go/web"
)

// Deps 聚合路由注册所需依赖。
type Deps struct {
	Cfg         *config.Config
	DB          *db.DB
	UserDAO     *dao.UserDAO
	JWTManager  *crypto.JWTManager
	AuthService *service.AuthService
	MailService *service.MailService
	AttachStore *attachment.Store

	// 阶段 5 新增（nil 表示不注册对应路由组）
	APIKeyService *service.APIKeyService // /api/auth/api-keys + /api/v1/send
	AdminService  *service.AdminService  // /api/admin/*
	RuleService   *service.RuleService   // /api/rules/*

	// 阶段 6 新增
	WSHub *ws.Hub // /ws WebSocket 升级
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
	registerMailRoutes(r, deps)

	// ===== 阶段 5：API Key + 管理员 + 规则 + API v1 =====
	// API Key 管理端点（用户维度，/api/auth/api-keys/*）
	if deps.Cfg.FeatureAPIKey && deps.APIKeyService != nil {
		registerAPIKeyRoutes(r, deps)
	}

	// 管理员后台端点（/api/admin/*）
	if deps.AdminService != nil {
		registerAdminRoutes(r, deps)
	}

	// API v1 外部发信端点（/api/v1/*，API Key 认证）
	if deps.Cfg.FeatureAPIKey && deps.APIKeyService != nil {
		registerAPIV1Routes(r, deps)
	}

	// 规则端点（/api/rules/*）
	if deps.Cfg.FeatureRules && deps.RuleService != nil {
		registerRuleRoutes(r, deps)
	}

	// ===== 阶段 6：WebSocket 实时推送 =====
	if deps.WSHub != nil {
		wsH := handler.NewWSHandler(deps.WSHub, deps.JWTManager, deps.UserDAO)
		r.GET("/ws", wsH.Upgrade)
	}

	// ===== 阶段 8：前端 SPA 静态文件服务（//go:embed dist/） =====
	registerSPARoutes(r)

	return r
}

// registerSPARoutes 注册前端 SPA 静态文件服务。
//
// 通过 //go:embed 将前端 dist/ 嵌入二进制，运行时由 Go 直接服务静态资源。
// SPA 路由策略：
//   - /api/*、/ws、/healthz 等基础设施路径 → JSON 404
//   - 存在的静态文件 → 直接返回（如 /assets/index-xxx.js）
//   - 其他路径 → 返回 index.html（交由 Vue Router 处理前端路由）
//
// 开发环境下 dist/ 仅含 .gitkeep，index.html 不存在，所有非 API 路径返回 JSON 404。
func registerSPARoutes(r *gin.Engine) {
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		slog.Error("前端 dist/ 嵌入失败，SPA 静态服务不可用", "error", err)
		return
	}

	fileServer := http.FileServer(http.FS(distFS))
	// 预读 index.html 用于 SPA fallback；开发环境下为空
	indexHTML, _ := fs.ReadFile(distFS, "index.html")

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// 基础设施路径返回 JSON 404（不返回 index.html，避免 SPA 劫持 API 404）
		if strings.HasPrefix(path, "/api/") || path == "/ws" ||
			path == "/healthz" || path == "/readyz" || path == "/startupz" ||
			path == "/health" || strings.HasPrefix(path, "/metrics") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// 尝试服务静态文件
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath != "" {
			if f, err := distFS.Open(cleanPath); err == nil {
				f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		// SPA fallback：返回 index.html（交由 Vue Router 处理）
		if len(indexHTML) > 0 {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			return
		}

		// 开发环境下无 index.html，返回 JSON 404
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})
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

// registerMailRoutes 注册 /api/mail/* 路由（阶段 3）。
//
// 端点路径分两类注册，避免 Gin 路由树静态/动态冲突：
//  1. 固定路径（list/unread-count/send/save-draft/empty-trash/batch/*/upload/*）
//  2. 动态路径 /:id/* —— 所有 :id 端点前置 middleware.RequireOwnedMail（P0-3 修复）
//
// 鉴权：所有 /api/mail/* 端点必须经过 Authenticate 中间件。
func registerMailRoutes(r *gin.Engine, deps Deps) {
	mailH := handler.NewMailHandler(deps.MailService, deps.AttachStore)

	mail := r.Group("/api/mail")
	mail.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
	{
		// ===== 固定路径端点 =====
		mail.GET("/list", mailH.List)
		mail.GET("/unread-count", mailH.UnreadCount)
		mail.POST("/send", mailH.Send)
		mail.POST("/save-draft", mailH.SaveDraft)
		mail.POST("/empty-trash", mailH.EmptyTrash)

		// 批量操作端点（补齐前端期望的端点）
		mail.POST("/batch/mark-read", mailH.BatchMarkRead)
		mail.POST("/batch/move", mailH.BatchMove)
		mail.POST("/batch/delete", mailH.BatchDelete)

		// ===== 动态路径 /:id 端点 =====
		// 所有 :id 操作前置 RequireOwnedMail（P0-3 修复：归属校验）
		owned := mail.Group("/:id", middleware.RequireOwnedMail(deps.MailService))
		{
			owned.GET("", mailH.Get)
			owned.PUT("/read", mailH.MarkRead)
			owned.PUT("/unread", mailH.MarkUnread)
			owned.PUT("/star", mailH.ToggleStar)
			owned.DELETE("", mailH.Delete)
			owned.PUT("/restore", mailH.Restore)

			// 附件下载（:aid 二次校验由 handler 内部完成）
			owned.GET("/attachments/download-all", mailH.DownloadAllAttachments)
			owned.GET("/attachments/:aid/download", mailH.DownloadAttachment)
		}
	}
}

// registerAPIKeyRoutes 注册 /api/auth/api-keys/* 路由（阶段 5）。
//
// 用户维度，JWT 认证（非 API Key 认证）。handler 通过 middleware.CurrentUser 取当前用户。
// 端点：
//
//	POST   /api/auth/api-keys        - 创建 API Key（明文仅返回一次）
//	GET    /api/auth/api-keys        - 列出当前用户的 API Key
//	DELETE /api/auth/api-keys/:id    - 删除 API Key（校验归属）
func registerAPIKeyRoutes(r *gin.Engine, deps Deps) {
	apikeyH := handler.NewAPIKeyHandler(deps.APIKeyService)

	g := r.Group("/api/auth/api-keys")
	g.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
	{
		g.POST("", apikeyH.CreateKey)
		g.GET("", apikeyH.ListKeys)
		g.DELETE("/:id", apikeyH.DeleteKey)
	}
}

// registerAdminRoutes 注册 /api/admin/* 路由（阶段 5）。
//
// 鉴权链：Authenticate（JWT）→ RequireAdmin（role=admin）。
// 端点：
//
//	GET    /api/admin/stats           - 用户统计
//	GET    /api/admin/users           - 用户列表
//	GET    /api/admin/users/:id       - 用户详情
//	POST   /api/admin/users           - 创建用户
//	PUT    /api/admin/users/:id       - 更新用户
//	DELETE /api/admin/users/:id       - 软删除用户
//	GET    /api/admin/settings        - 查询全局设置
//	PUT    /api/admin/settings        - 更新全局设置
func registerAdminRoutes(r *gin.Engine, deps Deps) {
	adminH := handler.NewAdminHandler(deps.AdminService)

	g := r.Group("/api/admin")
	g.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
	g.Use(middleware.RequireAdmin())
	{
		g.GET("/stats", adminH.Stats)
		g.GET("/users", adminH.ListUsers)
		g.GET("/users/:id", adminH.GetUser)
		g.POST("/users", adminH.CreateUser)
		g.PUT("/users/:id", adminH.UpdateUser)
		g.DELETE("/users/:id", adminH.DeleteUser)
		g.GET("/settings", adminH.GetSettings)
		g.PUT("/settings", adminH.UpdateSettings)
	}
}

// registerAPIV1Routes 注册 /api/v1/* 路由（阶段 5）。
//
// 鉴权链：APIKeyAuth（Bearer mk_xxx）→ APIKeyRateLimiter（per-key 限流）→ RequireScope("send")。
// 端点：
//
//	POST /api/v1/send - 外部发信
func registerAPIV1Routes(r *gin.Engine, deps Deps) {
	v1H := handler.NewAPIV1Handler(deps.MailService)
	rateLimiter := middleware.NewAPIKeyRateLimiter()

	g := r.Group("/api/v1")
	g.Use(middleware.APIKeyAuth(deps.APIKeyService, deps.UserDAO))
	g.Use(rateLimiter.RateLimit())
	{
		g.POST("/send", middleware.RequireScope("send"), v1H.Send)
	}
}

// registerRuleRoutes 注册 /api/rules/* 路由（阶段 5）。
//
// 用户维度，JWT 认证。handler 通过 middleware.CurrentUser 取当前用户。
// 端点：
//
//	GET    /api/rules        - 列出当前用户的规则
//	POST   /api/rules        - 创建规则
//	PUT    /api/rules/:id    - 更新规则（校验归属）
//	DELETE /api/rules/:id    - 删除规则（校验归属）
func registerRuleRoutes(r *gin.Engine, deps Deps) {
	ruleH := handler.NewRuleHandler(deps.RuleService)

	g := r.Group("/api/rules")
	g.Use(middleware.Authenticate(deps.JWTManager, deps.UserDAO))
	{
		g.GET("", ruleH.List)
		g.POST("", ruleH.Create)
		g.PUT("/:id", ruleH.Update)
		g.DELETE("/:id", ruleH.Delete)
	}
}
