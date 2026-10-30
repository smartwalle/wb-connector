// Package logging 负责全局 slog logger 的初始化。
//
// 该包基于标准库 log/slog：初始化后 slog 的默认 logger 就接管了全局输出，
// 业务代码直接用 slog 记录日志，不需要再包一层日志函数。
//
// 日志文案约定：
//   - 日志消息（msg）使用中文，方便人读；
//   - 日志字段名（key）保持英文，方便日志平台按字段检索与统计，
//     字段名是稳定的机器可读标识，不要随意改名；
//   - 字段值保持原样：SQL 语句、配置项路径、错误码、第三方库名等
//     机器可读的内容不做翻译，避免与上游约定对不上；
//   - 错误文案（fmt.Errorf、errors.New 的文本）不在日志范围内，保持原样，
//     日志里作为错误详情出现时按原样透传。
package logging

import (
	"context"
	"log/slog"
	"os"
	"wb-connector/internal/foundation/metadata"

	"github.com/smartwalle/logkit"
)

// Setup 按配置初始化全局 slog logger。
//
// slog.SetDefault 会同时接管标准 log 包的输出，
// 因此第三方库通过 log.Print 打出的日志也会按同样的格式输出。
func Setup(cfg Config) error {
	var handler = logkit.NewEnrichHandler(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{AddSource: true}),
		func(ctx context.Context) []slog.Attr {
			var rid = metadata.RequestIdFrom(ctx)
			if rid == "" {
				return nil
			}
			return []slog.Attr{slog.String("requestId", rid)}
		},
	)

	slog.SetDefault(slog.New(handler))
	return nil
}
