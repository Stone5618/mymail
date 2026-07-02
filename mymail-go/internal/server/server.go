// Package server 负责服务器编排：初始化 DB、HTTP 路由、启动与优雅关闭。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/httpapi"
	"github.com/mymail/mymail-go/internal/storage/db"
	"github.com/mymail/mymail-go/internal/tracing"
)

// Server 持有应用全部运行时依赖。
type Server struct {
	cfg     *config.Config
	db      *db.DB
	httpSrv *http.Server
	tracer  *tracing.Tracer
}

// New 构建服务器实例，初始化 DB、迁移、追踪、路由。
func New(ctx context.Context, cfg *config.Config) (*Server, error) {
	// 1. 初始化数据库连接 + 自动迁移
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := database.Migrate(); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	slog.Info("数据库已就绪", "path", cfg.DBPath)

	// 2. 初始化 OpenTelemetry 追踪（可选）
	var tr *tracing.Tracer
	if cfg.TracingEnabled && cfg.TracingEndpoint != "" {
		tr, err = tracing.Init(ctx, cfg.TracingEndpoint, "mymail", cfg.TracingSampleRate)
		if err != nil {
			slog.Warn("追踪初始化失败，继续运行", "error", err)
		} else {
			slog.Info("追踪已启用", "endpoint", cfg.TracingEndpoint)
		}
	}

	// 3. 构建 HTTP 路由
	router := httpapi.NewRouter(cfg, database)

	// 4. 构建 HTTP Server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	return &Server{
		cfg:     cfg,
		db:      database,
		httpSrv: httpSrv,
		tracer:  tr,
	}, nil
}

// Start 启动 HTTP 服务（阻塞调用）。
func (s *Server) Start() error {
	slog.Info("HTTP 服务监听中", "addr", s.httpSrv.Addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown 优雅关闭：停止接收新请求、等待在途请求完成、关闭 DB。
func (s *Server) Shutdown(ctx context.Context) error {
	// 1. 停止 HTTP 服务
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		slog.Error("HTTP 关闭失败", "error", err)
	}

	// 2. 关闭追踪 exporter
	if s.tracer != nil {
		if err := s.tracer.Shutdown(ctx); err != nil {
			slog.Error("追踪关闭失败", "error", err)
		}
	}

	// 3. 关闭数据库
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			slog.Error("数据库关闭失败", "error", err)
		}
	}

	return nil
}
