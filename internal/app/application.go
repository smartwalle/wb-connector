// Package app 负责依赖组装与生命周期协调。
//
// 应用层只做三件事：按依赖方向创建实现、把实现注入给使用方、
// 用 bootstrap 管理启动与关闭。
package app

import (
	"context"
	"errors"
	"fmt"

	"wb-connector/internal/service"

	"github.com/smartwalle/bootstrap"

	"wb-connector/internal/transport/http"
	"wb-connector/internal/transport/mcp"
)

// Run 组装依赖并启动服务，阻塞直到收到停止信号、上下文取消或启动失败。
//
// Environment 持有数据库等资源，由它负责创建与释放；
// 这里只负责把实现组装起来，并交给 bootstrap 管理生命周期。
func Run(ctx context.Context, env *Environment) error {
	if env == nil {
		return errors.New("environment is required")
	}

	var greetService = service.NewGreetService()

	// 认证服务。它同时服务于两处：OAuth 端点用它签发令牌，
	// MCP 端点的令牌校验中间件用它验签。
	var authService = service.NewAuthService()

	// MCP 协议服务。它只提供处理器，由 HTTP 层挂到 /mcp 路径上，
	// 与业务接口共用同一个 server、同一个端口。
	var mcpServer = mcp.NewServer(greetService)

	// HTTP 服务的监听与启停由传输层的 Server 管理，应用层只负责把它注册到生命周期框架。
	var httpServer = http.NewServer(env.Config.HTTP, mcpServer.HTTPHandler(), authService)

	var application = bootstrap.New(
		bootstrap.WithContext(ctx),
		bootstrap.WithServers(httpServer),
		bootstrap.WithStopTimeout(env.Config.App.ShutdownTimeout),
	)

	if err := application.Run(); err != nil {
		return fmt.Errorf("run application: %w", err)
	}
	return nil
}
