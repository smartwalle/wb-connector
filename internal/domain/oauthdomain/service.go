package oauthdomain

import (
	"context"
	"errors"
)

// OAuth 相关的领域错误。
var (
	// ErrInvalidAccount 表示账号或密码不正确。
	ErrInvalidAccount = errors.New("invalid account or password")

	// ErrInvalidClient 表示客户端未注册或已失效。
	ErrInvalidClient = errors.New("invalid client")

	// ErrInvalidRedirectURI 表示回调地址不在客户端登记的列表里，
	// 也不是允许的本机回环回调地址。
	ErrInvalidRedirectURI = errors.New("invalid redirect uri")

	// ErrInvalidAuthCode 表示授权码不存在、已使用或已过期。
	ErrInvalidAuthCode = errors.New("invalid authorization code")

	// ErrInvalidCodeVerifier 表示 PKCE 校验失败。
	ErrInvalidCodeVerifier = errors.New("invalid code verifier")

	// ErrUnsupportedChallengeMethod 表示不支持的 PKCE 挑战算法。
	ErrUnsupportedChallengeMethod = errors.New("unsupported code challenge method")

	// ErrInvalidToken 表示访问令牌无效或已过期。
	ErrInvalidToken = errors.New("invalid token")

	// ErrUnsupportedGrant 表示请求的授权类型不受支持。
	ErrUnsupportedGrant = errors.New("unsupported grant type")
)

// AuthenticateRequest 是"校验账号密码"用例的输入契约。
type AuthenticateRequest struct {
	// Username 是登录账号。
	Username string `json:"username"`

	// Password 是登录密码。
	Password string `json:"password"`
}

// AuthorizeRequest 是"签发授权码"用例的输入契约。
type AuthorizeRequest struct {
	// ClientID 是发起授权的客户端标识。
	ClientID string `json:"client_id"`

	// RedirectURI 是本次授权使用的回调地址。
	RedirectURI string `json:"redirect_uri"`

	// Scope 是请求的权限范围。
	Scope string `json:"scope,omitempty"`

	// UserID 是登录成功的用户标识。
	UserID string `json:"user_id"`

	// CodeChallenge 是 PKCE 的挑战值。
	CodeChallenge string `json:"code_challenge"`

	// CodeChallengeMethod 是挑战算法，必须为 S256。
	CodeChallengeMethod string `json:"code_challenge_method"`
}

// TokenRequest 是"签发访问令牌"用例的输入契约。
type TokenRequest struct {
	// GrantType 是授权类型，授权码流程为 authorization_code，刷新为 refresh_token。
	GrantType string `json:"grant_type"`

	// Code 是授权码，授权码流程必填。
	Code string `json:"code,omitempty"`

	// RedirectURI 是授权时使用的回调地址，换令牌时必须与之一致。
	RedirectURI string `json:"redirect_uri,omitempty"`

	// ClientID 是客户端标识。
	ClientID string `json:"client_id,omitempty"`

	// CodeVerifier 是 PKCE 校验值，与授权码绑定。
	CodeVerifier string `json:"code_verifier,omitempty"`

	// RefreshToken 是刷新令牌，刷新流程必填。
	RefreshToken string `json:"refresh_token,omitempty"`
}

// RegisterRequest 是"动态注册客户端"用例的输入契约。
type RegisterRequest struct {
	// RedirectURIs 是客户端请求的回调地址列表。
	RedirectURIs []string `json:"redirect_uris"`

	// ClientName 是客户端名称。
	ClientName string `json:"client_name,omitempty"`

	// GrantTypes 是客户端请求的授权类型列表。
	GrantTypes []string `json:"grant_types,omitempty"`

	// ResponseTypes 是客户端请求的响应类型列表。
	ResponseTypes []string `json:"response_types,omitempty"`

	// TokenEndpointAuthMethod 是客户端请求的令牌端点认证方式。
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

// Service 是 OAuth 模块的业务契约，每个方法是一个业务用例。
//
// 实现由 internal/service 提供，在应用组装层注入。
//
// 契约不感知传输方式：MCP 的令牌校验与 OAuth 端点的签发共用同一份实现。
type Service interface {
	// Authenticate 校验账号密码，成功返回用户标识。
	//
	// 账号不存在与密码错误返回同一个错误，避免暴露账号是否存在。
	Authenticate(ctx context.Context, req AuthenticateRequest) (string, error)

	// RegisterClient 动态注册客户端，返回新分配的 client_id。
	RegisterClient(ctx context.Context, req RegisterRequest) (*Client, error)

	// ValidateAuthorizeRequest 校验授权请求的参数是否合法。
	//
	// 校验客户端是否已注册、回调地址是否在登记列表内、PKCE 参数是否合法。
	// 渲染登录页前先调用它，避免把用户引向非法回调地址。
	//
	// 回调地址除登记列表内的精确匹配外，还允许本机回环上的 OAuth 回调地址，
	// 端口可与登记值不同——原生应用的回环端口由客户端运行时分配。
	ValidateAuthorizeRequest(ctx context.Context, req AuthorizeRequest) error

	// IssueAuthCode 为已登录的用户签发授权码。
	//
	// 客户端未注册、回调地址不匹配或 PKCE 参数不合法时返回错误。
	IssueAuthCode(ctx context.Context, req AuthorizeRequest) (*AuthCode, error)

	// IssueToken 用授权码或刷新令牌换取访问令牌。
	//
	// 授权码一次性使用；PKCE 校验不通过时返回 ErrInvalidCodeVerifier。
	// 授权码与刷新令牌都必须与 client_id 匹配。
	IssueToken(ctx context.Context, req TokenRequest) (*Token, error)

	// RevokeToken 撤销令牌。
	//
	// 令牌不存在时也返回成功——让调用方无法通过响应差异探测令牌是否存在。
	// 撤销访问令牌时，其配套的刷新令牌一并失效。
	RevokeToken(ctx context.Context, req RevokeRequest) error

	// VerifyToken 校验访问令牌，返回令牌携带的用户标识与权限范围。
	//
	// 令牌无效或已过期时返回 ErrInvalidToken。
	VerifyToken(ctx context.Context, accessToken string) (*Token, error)

	// Issuer 返回授权服务器标识。
	//
	// 它是元数据里各端点地址的前缀，也是客户端发现授权服务器的依据。
	Issuer() string

	// Resource 返回受保护资源的标识。
	//
	// 它是客户端申请令牌时指定的目标资源。
	Resource() string
}
