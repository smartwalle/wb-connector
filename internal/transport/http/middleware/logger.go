package middleware

import (
	"log/slog"
	"time"

	"github.com/smartwalle/rx"
)

// MidLogger 是基于 slog 的访问日志中间件。
func MidLogger() rx.HandlerFunc {
	return func(c *rx.Context) {
		var start = time.Now()
		c.Next()
		slog.InfoContext(c.Context(),
			"HTTP 请求",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(start).String(),
		)
	}
}
