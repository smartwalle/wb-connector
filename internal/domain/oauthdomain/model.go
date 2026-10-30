// Package oauthdomain 定义 OAuth 领域的实体、契约与配置。
//
// 这里只描述"授权码、令牌、客户端注册"这些概念本身，
// 不感知 HTTP 协议细节，也不感知 MCP。
package oauthdomain

import "time"

// Client 是通过动态注册登记的 OAuth 客户端。
type Client struct {
	// ClientID 是客户端标识，由服务端在注册时生成。
	ClientID string `json:"client_id"`

	// RedirectURIs 是客户端允许的回调地址列表。
	//
	// 授权请求里的 redirect_uri 必须与其中某一项字符串精确匹配。
	RedirectURIs []string `json:"redirect_uris"`

	// ClientName 是客户端名称，用于展示。
	ClientName string `json:"client_name,omitempty"`

	// CreatedAt 是注册时间。
	CreatedAt time.Time `json:"client_id_issued_at"`
}

// AuthCode 是授权码。
//
// 授权码一次性使用，有效期约 10 分钟，与 PKCE 的 code_challenge 绑定。
type AuthCode struct {
	// Code 是授权码本身。
	Code string `json:"code"`

	// ClientID 是发起授权的客户端标识。
	ClientID string `json:"client_id"`

	// RedirectURI 是本次授权使用的回调地址，换取令牌时必须一致。
	RedirectURI string `json:"redirect_uri"`

	// Scope 是本次授权授予的权限范围。
	Scope string `json:"scope,omitempty"`

	// UserID 是登录成功的用户标识。
	UserID string `json:"user_id"`

	// CodeChallenge 是 PKCE 的挑战值，换令牌时用 code_verifier 校验。
	CodeChallenge string `json:"code_challenge"`

	// CodeChallengeMethod 是挑战算法，只支持 S256。
	CodeChallengeMethod string `json:"code_challenge_method"`

	// ExpiresAt 是过期时间。
	ExpiresAt time.Time `json:"expires_at"`
}

// IsExpired 判断授权码是否已过期。
func (c AuthCode) IsExpired(now time.Time) bool {
	return now.After(c.ExpiresAt)
}

// Token 是访问令牌。
type Token struct {
	// AccessToken 是访问令牌本身。
	AccessToken string `json:"access_token"`

	// RefreshToken 是刷新令牌，用于换取新的访问令牌。
	RefreshToken string `json:"refresh_token"`

	// ClientID 是令牌所属的客户端标识。
	//
	// 刷新令牌必须绑定到客户端：否则拿到刷新令牌的一方，
	// 无论以哪个客户端的身份，都能无限续期。
	ClientID string `json:"client_id"`

	// TokenType 是令牌类型，固定为 Bearer。
	TokenType string `json:"token_type"`

	// Scope 是授予的权限范围。
	Scope string `json:"scope,omitempty"`

	// UserID 是令牌对应的用户标识。
	UserID string `json:"user_id"`

	// ExpiresAt 是访问令牌过期时间。
	ExpiresAt time.Time `json:"expires_at"`

	// RefreshExpiresAt 是刷新令牌过期时间。
	RefreshExpiresAt time.Time `json:"refresh_token_expires_at"`
}

// IsExpired 判断访问令牌是否已过期。
func (t Token) IsExpired(now time.Time) bool {
	return now.After(t.ExpiresAt)
}

// RevokeRequest 是"撤销令牌"用例的输入契约。
//
// 对应 RFC 7009 的令牌撤销端点。
type RevokeRequest struct {
	// Token 是要撤销的令牌，可能是访问令牌或刷新令牌。
	Token string `json:"token"`

	// TokenTypeHint 是令牌类型提示，取值为 access_token 或 refresh_token。
	//
	// 只是提示，服务器忽略它也能凭令牌内容定位。
	TokenTypeHint string `json:"token_type_hint,omitempty"`

	// ClientID 是发起撤销的客户端标识，用于校验令牌归属。
	ClientID string `json:"client_id,omitempty"`
}
