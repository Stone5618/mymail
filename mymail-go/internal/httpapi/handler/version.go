// Package handler
// version.go 实现版本信息端点。
//
// 端点：
//   GET /api/version - 返回后端版本信息（公开端点，无需认证）
package handler

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
)

// VersionHandler 处理版本信息端点。
type VersionHandler struct {
	version   string
	buildTime string
	commitSHA string
}

// NewVersionHandler 创建版本信息处理器。
func NewVersionHandler(version, buildTime, commitSHA string) *VersionHandler {
	return &VersionHandler{
		version:   version,
		buildTime: buildTime,
		commitSHA: commitSHA,
	}
}

// GetVersion 返回后端版本信息。
// GET /api/version
func (h *VersionHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"backend_version": h.version,
		"go_version":      runtime.Version(),
		"build_time":      h.buildTime,
		"commit_sha":      h.commitSHA,
	})
}
