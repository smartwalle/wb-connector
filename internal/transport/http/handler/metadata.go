// Package handler 是业务接口的 HTTP 适配层。
//
// 这里只做协议适配：绑定请求、调用用例、输出结果。
// 业务错误到状态码的映射由 jsoncodec 统一完成。
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// 元数据端点的路径。
//
// 这两个路径由规范固定，客户端据此发现授权服务器。
const (
	// PathProtectedResource 是受保护资源元数据端点。
	PathProtectedResource = "/.well-known/oauth-protected-resource"

	// PathAuthorizationServer 是授权服务器元数据端点。
	PathAuthorizationServer = "/.well-known/oauth-authorization-server"
)

// MetadataHandler 提供 OAuth 元数据端点。
//
// 它与授权流程端点分开：元数据是给客户端"发现"用的静态描述，
// 不参与授权流程，路径规则也由规范固定。
type MetadataHandler struct {
	// issuer 是授权服务器标识，同时用作元数据里各端点的前缀。
	issuer string

	// resource 是受保护资源的标识，通常是 MCP 端点的 URL。
	resource string
}

// NewMetadataHandler 创建 MetadataHandler。
//
// issuer 是对外可达的授权服务器地址，例如 https://mcp.example.com；
// resource 是受保护资源的地址，通常与 MCP 端点一致。
func NewMetadataHandler(issuer string, resource string) *MetadataHandler {
	return &MetadataHandler{
		issuer:   strings.TrimSuffix(issuer, "/"),
		resource: resource,
	}
}

// RegisterRoutes 声明本 MetadataHandler 负责的路由。
//
// 两个端点都在 origin 根下：客户端按 RFC 9728 与 RFC 8414 的规则
// 从服务地址推导它们的位置。
func (h *MetadataHandler) RegisterRoutes(r router.Router) {
	r.GET(PathProtectedResource, h.ProtectedResource)
	r.GET(PathAuthorizationServer, h.AuthorizationServer)
}

// ProtectedResource 返回受保护资源元数据。
//
// 客户端收到 401 后从这里发现授权服务器地址。
func (h *MetadataHandler) ProtectedResource(c *rx.Context) {
	h.writeJSON(c, http.StatusOK, map[string]any{
		"resource":              h.resource,
		"authorization_servers": []string{h.issuer},
	})
}

// AuthorizationServer 返回授权服务器元数据。
func (h *MetadataHandler) AuthorizationServer(c *rx.Context) {
	// 注意这里不声明 revocation_endpoint。
	//
	// WorkBuddy 客户端要求撤销端点必须是 https，否则会判为 unsafe_endpoint
	// 并让撤销流程报错；而本地开发用的是 http 授权服务器。
	// 端点本身已经实现（/oauth/revoke），部署到 https 后在元数据里
	// 加上 "revocation_endpoint" 即可启用客户端的自动撤销。
	h.writeJSON(c, http.StatusOK, map[string]any{
		"issuer":                                h.issuer,
		"authorization_endpoint":                h.issuer + PathAuthorize,
		"token_endpoint":                        h.issuer + PathToken,
		"registration_endpoint":                 h.issuer + PathRegister,
		"scopes_supported":                      []string{},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{grantTypeAuthorizationCode, grantTypeRefreshToken},
		"code_challenge_methods_supported":      []string{codeChallengeMethodS256},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// writeJSON 写出 JSON 响应。
func (h *MetadataHandler) writeJSON(c *rx.Context, status int, body any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(status)
	if err := json.NewEncoder(c.Writer).Encode(body); err != nil {
		slog.ErrorContext(c.Context(), "写出 JSON 响应失败", slog.Any("error", err))
	}
}
