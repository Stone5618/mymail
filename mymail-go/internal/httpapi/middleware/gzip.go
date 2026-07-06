// Package middleware 提供 HTTP 中间件。
package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipResponseWriter 包装 gin.ResponseWriter，按需对写入的数据做 gzip 压缩。
// 在第一次 Write 时根据 Content-Type 决定是否启用压缩。
type gzipResponseWriter struct {
	gin.ResponseWriter
	gz        *gzip.Writer
	compress  bool   // 是否启用压缩
	checked   bool   // 是否已检查 Content-Type
	contentCT string // 缓存的 Content-Type
}

func (g *gzipResponseWriter) Write(data []byte) (int, error) {
	// 首次写入时检查 Content-Type，决定是否压缩
	if !g.checked {
		g.checked = true
		ct := g.Header().Get("Content-Type")
		g.compress = shouldCompress(ct) && len(data) >= 1024
		if g.compress {
			g.Header().Set("Content-Encoding", "gzip")
			g.Header().Add("Vary", "Accept-Encoding")
			// 压缩后长度不确定，移除 Content-Length
			g.Header().Del("Content-Length")
		}
	}

	if g.compress {
		return g.gz.Write(data)
	}
	return g.ResponseWriter.Write(data)
}

func (g *gzipResponseWriter) WriteString(s string) (int, error) {
	return g.Write([]byte(s))
}

// gzipWriterPool 复用 gzip.Writer 对象，减少 GC 压力。
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// shouldCompress 判断响应类型是否值得压缩。
// 跳过已压缩格式（图片、字体、视频等），只压缩文本类资源。
func shouldCompress(contentType string) bool {
	contentType = strings.ToLower(contentType)
	for _, t := range []string{
		"text/",
		"application/javascript",
		"application/x-javascript",
		"application/json",
		"application/xml",
		"application/rss",
		"image/svg+xml",
	} {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}

// Gzip 对文本类响应做 gzip 压缩，减少传输体积。
// 仅当客户端发送 Accept-Encoding: gzip 且响应类型为文本时启用。
// 已压缩格式（图片、字体、视频）自动跳过。
func Gzip() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查客户端是否支持 gzip
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		// HEAD 请求无需压缩
		if c.Request.Method == http.MethodHead {
			c.Next()
			return
		}

		// 从池中获取 gzip.Writer
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(c.Writer)

		// 包装 Writer
		origWriter := c.Writer
		gw := &gzipResponseWriter{
			ResponseWriter: origWriter,
			gz:             gz,
		}
		c.Writer = gw

		c.Next()

		// 确保 gzip.Writer 刷盘并归还池
		if gw.compress {
			gz.Flush()
		}
		gz.Reset(io.Discard)
		gzipWriterPool.Put(gz)
	}
}
