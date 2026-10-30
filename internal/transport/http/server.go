// Package http 是 HTTP 传输层：创建 Engine、组装路由、配置中间件，
// 并用 Server 管理 HTTP 服务的监听与启停。
//
// 统一响应格式与业务错误的映射在 jsonrender 里完成。
package http

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"wb-connector/internal/domain/oauthdomain"
	"wb-connector/internal/foundation/errors"
	"wb-connector/internal/transport/http/handler"
	"wb-connector/internal/transport/http/jsoncodec"
	"wb-connector/internal/transport/http/middleware"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// Server 管理 HTTP 服务：rx Engine、路由与中间件，以及 HTTP Server 的监听与启停。
//
// 它满足应用生命周期框架的服务契约（Start/Stop），由 internal/app 注册到 bootstrap。
type Server struct {
	config Config
	server *http.Server
}

// NewServer 创建 HTTP 服务。
//
// config 提供地址与超时参数，mcpHandler 是 MCP 协议的处理器，挂在 mcpPath 指定的路径上
// 与业务接口共用同一个端口。
//
// oauthService 非空时启用 OAuth：注册授权端点，并给 MCP 端点加上令牌校验。
func NewServer(config Config, mcpHandler http.Handler, oauthService oauthdomain.Service) *Server {
	var engine = rx.New()
	engine.Use(middleware.MidRecovery())
	engine.Use(middleware.MidRequestId())
	engine.Use(middleware.MidLogger())
	engine.UseErrorEncoder(jsoncodec.ErrorEncoder())
	engine.UseNotFoundHandler(jsoncodec.NotFoundHandler())

	// 新增业务接口时，在这里注册对应的 Handler。
	var r = router.New()
	handler.NewHealthHandler().RegisterRoutes(r)

	// 元数据端点：客户端据此发现授权服务器。
	handler.NewMetadataHandler(oauthService).RegisterRoutes(r)

	// 授权流程端点：注册、授权、登录、令牌、撤销。
	handler.NewOAuthHandler(oauthService).RegisterRoutes(r)

	// MCP服务端点
	var mcpGroup = r.Group("/mcp")
	mcpGroup.Use(middleware.MidOAuth(oauthService, handler.PathProtectedResource))
	mcpGroup.POST("", func(c *rx.Context) {
		mcpHandler.ServeHTTP(c.Writer, c.Request)
	})
	mcpGroup.GET("", func(c *rx.Context) {
		mcpHandler.ServeHTTP(c.Writer, c.Request)
	})

	engine.UseRouter(r)

	return &Server{
		config: config,
		server: &http.Server{
			Addr:         config.Address,
			Handler:      engine,
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
			IdleTimeout:  config.IdleTimeout,
		},
	}
}

// Start 监听地址并提供服务，阻塞直到服务被关闭。
//
// 监听失败（地址非法、端口被占用）时返回错误，调用方据此判定启动失败。
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.config.Address)
	if err != nil {
		slog.ErrorContext(ctx, "HTTP 服务监听失败", slog.Any("error", err))
		return fmt.Errorf("listen on %s: %w", s.config.Address, err)
	}
	// 请求的 ctx 不跟随生命周期 ctx 取消：收到停止信号后，在途请求仍要跑完。
	s.server.BaseContext = func(net.Listener) context.Context {
		return context.WithoutCancel(ctx)
	}
	slog.InfoContext(ctx, "HTTP 服务开始运行", slog.String("address", listener.Addr().String()))
	if err = s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.ErrorContext(ctx, "HTTP 服务运行失败", slog.Any("error", err))
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}

// Stop 优雅关闭：停止接收新请求，并等待在途请求结束。
//
// 等待上限由调用方给的期限决定，到点后仍未结束的请求会被中断。
func (s *Server) Stop(ctx context.Context) error {
	slog.InfoContext(ctx, "HTTP 服务开始关闭")

	if err := s.server.Shutdown(ctx); err != nil {
		slog.ErrorContext(ctx, "HTTP 服务关闭失败", slog.Any("error", err))
		return fmt.Errorf("shutdown http server: %w", err)
	}
	slog.InfoContext(ctx, "HTTP 服务关闭完成")
	return nil
}
