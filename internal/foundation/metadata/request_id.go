package metadata

import "context"

type requestIdKey struct{}

func WithRequestId(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, requestIdKey{}, rid)
}

func RequestIdFrom(ctx context.Context) string {
	var rid, _ = ctx.Value(requestIdKey{}).(string)
	return rid
}
