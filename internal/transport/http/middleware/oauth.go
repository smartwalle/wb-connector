package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"wb-connector/internal/domain/metadata"

	"wb-connector/internal/domain/oauthdomain"

	"github.com/smartwalle/rx"
)

// HeaderWWWAuthenticate 是 WWW-Authenticate 响应头名称。
//
// 未授权时通过它告诉客户端去哪里获取令牌：MCP 客户端据此发现
// 受保护资源元数据，进而找到授权服务器并发起登录流程。
const HeaderWWWAuthenticate = "WWW-Authenticate"

// MidOAuth 校验请求携带的 Bearer 令牌。
//
// 它只挂在需要认证的路由上（如 MCP 端点），业务接口不受影响。
// 校验通过时把令牌携带的用户标识放进请求上下文，后续处理器可从中读取。
//
// 用法：作为路由处理器的外层，校验通过后调用 c.Next() 交给下一个处理器。
func MidOAuth(service oauthdomain.Service, resourceMetadataPath string) rx.HandlerFunc {
	// 客户端收到 401 后访问它，发现授权服务器并发起登录流程。
	var resourceMetadataURL = service.Issuer() + resourceMetadataPath

	return func(c *rx.Context) {
		var value = extractBearerToken(c.Request)
		if value == "" {
			challenge(c, resourceMetadataURL)
			c.Abort()
			return
		}

		var token, err = service.VerifyToken(c.Context(), value)
		if err != nil {
			slog.WarnContext(c.Context(), "令牌校验失败", slog.Any("error", err))
			challenge(c, resourceMetadataURL)
			c.Abort()
			return
		}

		c.UseContext(metadata.WithToken(c.Context(), token))
	}
}

// challenge 返回 401 并附带 WWW-Authenticate 头。
func challenge(c *rx.Context, resourceMetadataURL string) {
	var value = "Bearer"
	if resourceMetadataURL != "" {
		value += " resource_metadata=\"" + resourceMetadataURL + "\""
	}
	c.Writer.Header().Set(HeaderWWWAuthenticate, value)
	c.Writer.WriteHeader(http.StatusUnauthorized)
	c.Writer.Write(nil)
}

// extractBearerToken 从 Authorization 头里取出 Bearer 令牌。
func extractBearerToken(r *http.Request) string {
	var header = r.Header.Get("Authorization")
	var fields = strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}
