// Package main 是 MyMail 应用的入口。
// 负责加载配置、初始化日志、启动 HTTP 服务器并处理优雅关闭。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mymail/mymail-go/internal/config"
	"github.com/mymail/mymail-go/internal/logger"
	"github.com/mymail/mymail-go/internal/server"
)

// 构建时通过 -ldflags 注入。
var (
	Version   = "dev"
	BuildTime = ""
	CommitSHA = ""
)

func main() {
	// 1. 加载配置（含 P0-8 修复：JWT_SECRET 强制校验）
	cfg, err := config.Load()
	if err != nil {
		// 配置加载失败时 logger 尚未初始化，用标准 log 输出
		slog.Error("配置加载失败", "error", err)
		os.Exit(1)
	}
	cfg.Version = Version
	cfg.BuildTime = BuildTime
	cfg.CommitSHA = CommitSHA

	// 2. 初始化结构化日志（slog + trace_id 注入）
	logger.Init(cfg.LogLevel, cfg.LogFormat)
	slog.Info("MyMail 启动中",
		"version", Version,
		"build_time", BuildTime,
		"commit_sha", CommitSHA,
		"env", cfg.Env,
		"port", cfg.Port,
	)

	// 3. 创建根 context，监听终止信号
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 4. 构建并启动服务器
	srv, err := server.New(ctx, cfg)
	if err != nil {
		slog.Error("服务器初始化失败", "error", err)
		os.Exit(1)
	}

	// 在 goroutine 中启动 HTTP 服务
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP 服务异常", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("MyMail 已启动", "port", cfg.Port)

	// 5. 等待终止信号
	<-ctx.Done()
	slog.Info("收到终止信号，开始优雅关闭...")

	// 给予 30 秒优雅关闭窗口
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("优雅关闭失败", "error", err)
		os.Exit(1)
	}

	slog.Info("MyMail 已停止")
}
