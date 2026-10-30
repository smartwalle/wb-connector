// Package mcp 是 MCP 传输层：封装 MCP Server、注册工具，并把 MCP 协议暴露成可挂载的处理器。
//
// 本包不负责监听端口，也不引入独立的配置文件：
// 端点路径由常量 Path 定义，HTTP 传输由调用方挂到自己的路由上，
// 与其它 HTTP 接口共用同一个 server 与端口。
package mcp

import (
	"net/http"
	"wb-connector/internal/domain/greetdomain"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ServerName 是 MCP Server 对外暴露的名称。
	ServerName = "wb-connector"

	// ServerVersion 是 MCP Server 的版本号。
	ServerVersion = "1.0.0"
)

// Server 封装 MCP Server，负责创建实例与注册工具。
type Server struct {
	server       *mcp.Server
	handler      http.Handler
	greetService greetdomain.Service
}

// NewServer 创建 MCP Server 并注册工具。
//
// greetService 由应用组装层注入具体实现。
func NewServer(greetService greetdomain.Service) *Server {
	var s = &Server{
		server: mcp.NewServer(&mcp.Implementation{
			Name:    ServerName,
			Version: ServerVersion,
		}, nil),
		greetService: greetService,
	}

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "greet",
		Description: "向指定姓名的人问好，返回中文或英文问候语。",
		Annotations: &mcp.ToolAnnotations{
			Title:          "问候",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, s.Greet)

	s.handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server {
			return s.server
		},
		&mcp.StreamableHTTPOptions{
			Stateless: true,
		},
	)
	return s
}

func (s *Server) HTTPHandler() http.Handler {
	return s.handler
}
