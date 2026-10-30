package oauthdomain

// OAuth 协议中的固定取值。
//
// 端点路径不在这里：它们只有传输层用，与协议取值分开放。
const (
	// GrantTypeAuthorizationCode 是授权码流程的授权类型。
	GrantTypeAuthorizationCode = "authorization_code"

	// GrantTypeRefreshToken 是刷新流程的授权类型。
	GrantTypeRefreshToken = "refresh_token"

	// CodeChallengeMethodS256 是唯一支持的 PKCE 挑战算法。
	CodeChallengeMethodS256 = "S256"

	// ResponseTypeCode 是授权码流程的响应类型。
	ResponseTypeCode = "code"

	// TokenEndpointAuthMethodNone 表示令牌端点不要求客户端认证。
	TokenEndpointAuthMethodNone = "none"
)
