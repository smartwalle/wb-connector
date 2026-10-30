// Package handler 是业务接口的 HTTP 适配层。
//
// 这里只做协议适配：绑定请求、调用用例、输出结果。
// 业务错误到状态码的映射由 jsoncodec 统一完成。
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"wb-connector/internal/domain/oauthdomain"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// OAuth 端点的路径。
//
// 元数据端点由规范固定：客户端按 RFC 9728 与 RFC 8414 的规则
// 从服务地址推导它们的位置。
//
// 业务端点的路径由本服务约定，它们在元数据里以完整地址声明，
// 客户端照着访问即可。
const (
	// PathProtectedResource 是受保护资源元数据端点。
	PathProtectedResource = "/.well-known/oauth-protected-resource"

	// PathAuthorizationServer 是授权服务器元数据端点。
	PathAuthorizationServer = "/.well-known/oauth-authorization-server"

	// PathRegister 是动态客户端注册端点。
	PathRegister = "/oauth/register"

	// PathAuthorize 是授权端点。
	PathAuthorize = "/oauth/authorize"

	// PathLogin 是登录表单提交端点。
	PathLogin = "/oauth/login"

	// PathToken 是令牌端点。
	PathToken = "/oauth/token"

	// PathRevoke 是令牌撤销端点。
	PathRevoke = "/oauth/revoke"
)

// protectedResourceMetadata 是受保护资源元数据。
//
// 客户端收到 401 后访问它，从中发现授权服务器地址。
type protectedResourceMetadata struct {
	// Resource 是受保护资源的标识。
	Resource string `json:"resource"`

	// AuthorizationServers 是能签发本资源令牌的授权服务器地址。
	AuthorizationServers []string `json:"authorization_servers"`
}

// authorizationServerMetadata 是授权服务器元数据。
//
// 客户端据此发现授权、令牌、注册等端点的地址。
type authorizationServerMetadata struct {
	// Issuer 是授权服务器标识。
	Issuer string `json:"issuer"`

	// AuthorizationEndpoint 是授权端点地址。
	AuthorizationEndpoint string `json:"authorization_endpoint"`

	// TokenEndpoint 是令牌端点地址。
	TokenEndpoint string `json:"token_endpoint"`

	// RegistrationEndpoint 是动态客户端注册端点地址。
	RegistrationEndpoint string `json:"registration_endpoint"`

	// ScopesSupported 是支持的权限范围。
	ScopesSupported []string `json:"scopes_supported"`

	// ResponseTypesSupported 是支持的响应类型。
	ResponseTypesSupported []string `json:"response_types_supported"`

	// GrantTypesSupported 是支持的授权类型。
	GrantTypesSupported []string `json:"grant_types_supported"`

	// CodeChallengeMethodsSupported 是支持的 PKCE 挑战算法。
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// TokenEndpointAuthMethodsSupported 是支持的令牌端点认证方式。
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// MetadataHandler 提供 OAuth 元数据端点。
//
// 它与授权流程端点分开：元数据是给客户端"发现"用的静态描述，
// 不参与授权流程，路径规则也由规范固定。
type MetadataHandler struct {
	// service 是领域包定义的 oauthdomain.Service 契约。
	service oauthdomain.Service
}

// NewMetadataHandler 创建 MetadataHandler。
//
// service 提供元数据里要声明的授权服务器标识与受保护资源标识。
func NewMetadataHandler(service oauthdomain.Service) *MetadataHandler {
	return &MetadataHandler{service: service}
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
	h.writeJSON(c, http.StatusOK, protectedResourceMetadata{
		Resource:             h.service.Resource(),
		AuthorizationServers: []string{h.service.Issuer()},
	})
}

// AuthorizationServer 返回授权服务器元数据。
func (h *MetadataHandler) AuthorizationServer(c *rx.Context) {
	var issuer = h.service.Issuer()
	// 注意这里不声明 revocation_endpoint。
	//
	// WorkBuddy 客户端要求撤销端点必须是 https，否则会判为 unsafe_endpoint
	// 并让撤销流程报错；而本地开发用的是 http 授权服务器。
	// 端点本身已经实现（/oauth/revoke），部署到 https 后在元数据里
	// 加上 "revocation_endpoint" 即可启用客户端的自动撤销。
	h.writeJSON(c, http.StatusOK, authorizationServerMetadata{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + PathAuthorize,
		TokenEndpoint:                     issuer + PathToken,
		RegistrationEndpoint:              issuer + PathRegister,
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{oauthdomain.ResponseTypeCode},
		GrantTypesSupported:               []string{oauthdomain.GrantTypeAuthorizationCode, oauthdomain.GrantTypeRefreshToken},
		CodeChallengeMethodsSupported:     []string{oauthdomain.CodeChallengeMethodS256},
		TokenEndpointAuthMethodsSupported: []string{oauthdomain.TokenEndpointAuthMethodNone},
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
