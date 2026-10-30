// Package jsoncodec 是 rx 之上的 JSON 请求(返回)数据编解码器：请求绑定、业务错误映射与统一响应输出。
//
// 业务 Handler 只需要写"拿到入参、调用用例、返回数据或错误"，
// 其余都交给这里的 Handler。
package jsoncodec

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"wb-connector/internal/foundation/errors"

	"github.com/smartwalle/rx"
)

// Handler 绑定请求体、调用业务函数并输出结果。
//
// 业务函数的返回值就是响应里的 data；错误统一在这里映射成状态码与错误响应，
// 业务函数不用关心 HTTP 细节。
func Handler[T any, R any](fn func(c *rx.Context, req T) (R, error)) rx.HandlerFunc {
	return func(c *rx.Context) {
		var req T
		if err := decode(c, &req); err != nil {
			// 错误直接终止，需要在 rx.Engine 的 ErrorEncoder 中统一处理
			c.AbortWithError(errors.InvalidArgument("invalid argument"))
			return
		}

		var data, err = fn(c, req)
		if err != nil {
			// 错误直接终止，需要在 rx.Engine 的 ErrorEncoder 中统一处理
			c.AbortWithError(err)
			return
		}
		c.JSON(http.StatusOK, Response{Data: data})
	}
}

func decode(c *rx.Context, req any) error {
	if c.Request.Method != http.MethodPost {
		return nil
	}

	if c.Request.Body == nil || c.Request.Body == http.NoBody || c.Request.ContentLength == 0 {
		return io.EOF
	}
	defer func() {
		_ = c.Request.Body.Close()
	}()

	return json.NewDecoder(c.Request.Body).Decode(req)
}

// ErrorEncoder 错误编码器
func ErrorEncoder() rx.ErrorEncoder {
	return func(c *rx.Context, err error) {
		slog.ErrorContext(c.Context(), "请求处理失败",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Any("error", err),
		)

		// 业务错误统一处理
		var nErr errors.Error
		if errors.As(err, &nErr) {
			c.JSON(http.StatusBadRequest, Response{Code: nErr.Code(), Message: nErr.Message()})
			return
		}

		// 其它不可识别为业务错误的类型，统一响应“服务端内部错误”
		c.JSON(http.StatusInternalServerError, Response{Code: errors.CodeInternalServerError, Message: "internal server error"})
	}
}

func NotFoundHandler() rx.HandlerFunc {
	return func(c *rx.Context) {
		c.AbortWithError(errors.NotFound("route not found"))
	}
}
