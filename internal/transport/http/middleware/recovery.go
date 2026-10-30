package middleware

import (
	"log/slog"
	"runtime/debug"
	"wb-connector/internal/foundation/errors"

	"github.com/smartwalle/rx"
)

// MidRecovery 捕获 panic，避免连接被直接断开。
func MidRecovery() rx.HandlerFunc {
	return func(c *rx.Context) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(c.Context(), "捕获到 panic，请求已中断", "panic", r, "stack", string(debug.Stack()))
				c.AbortWithError(errors.New(errors.CodeInternalServerError, "internal server error"))
			}
		}()
		c.Next()
	}
}
