// Package middleware
package middleware

import (
	"context"

	"github.com/mymail/mymail-go/internal/logger"
)

// WithRequestID 将 request_id 注入 context。
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, logger.RequestIDKey, reqID)
}

// GetRequestID 从 context 中提取 request_id。
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(logger.RequestIDKey).(string); ok {
		return v
	}
	return ""
}
