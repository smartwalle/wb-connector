package metadata

import (
	"context"
	"wb-connector/internal/domain/oauthdomain"
)

type tokenKey struct{}

// WithToken 把已校验的令牌放进上下文。
//
// 令牌校验通过后由传输层的中间件调用，后续处理器可从中读取用户标识。
func WithToken(ctx context.Context, token *oauthdomain.Token) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// TokenFrom 从上下文取出已校验的令牌。
//
// 不存在时返回 nil，调用方需要自行处理未认证的情况。
func TokenFrom(ctx context.Context) *oauthdomain.Token {
	var token, _ = ctx.Value(tokenKey{}).(*oauthdomain.Token)
	return token
}
