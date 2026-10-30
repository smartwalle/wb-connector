package middleware

import (
	"uuid"
	"wb-connector/internal/foundation/metadata"

	"github.com/smartwalle/rx"
)

// HeaderRequestId 是请求 id 的请求头名称。
const HeaderRequestId = "Request-Id"

// MidRequestId 保证每个请求都有请求 id。
//
// 请求头带了 Request-Id 就沿用，没带就生成一个 UUID v4，
// 最终生效的请求 id 都会写进响应头。
func MidRequestId() rx.HandlerFunc {
	return func(c *rx.Context) {
		var rid = c.Request.Header.Get(HeaderRequestId)
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Writer.Header().Set(HeaderRequestId, rid)

		c.UseContext(metadata.WithRequestId(c.Context(), rid))

		c.Next()
	}
}
