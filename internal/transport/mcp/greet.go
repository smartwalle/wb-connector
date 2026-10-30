package mcp

import (
	"context"
	"fmt"
	"wb-connector/internal/domain/greetdomain"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GreetInput 是 greet 工具的入参。
//
// json tag 决定字段名，jsonschema tag 决定 AI 看到的参数描述，
// 官方 SDK 会据此自动生成入参 JSON Schema。
type GreetInput struct {
	// Name 为要问候的对象姓名，必填。
	Name string `json:"name" jsonschema:"要问候的对象姓名，必填，最长 64 个字符"`
	// Language 为问候语语言，可选，缺省为中文（zh），也可传 en。
	Language string `json:"language,omitempty" jsonschema:"问候语语言，可选。zh 为中文（默认），en 为英文"`
}

// GreetOutput 是 greet 工具的结构化输出。
type GreetOutput struct {
	// Message 为最终展示给用户的问候语文本。
	Message string `json:"message" jsonschema:"最终展示给用户的问候语文本"`
	// Language 为实际使用的语言。
	Language string `json:"language" jsonschema:"实际使用的语言，zh 或 en"`
	// Name 为归一化后的姓名。
	Name string `json:"name" jsonschema:"归一化（去除首尾空白）后的姓名"`
}

func (s *Server) Greet(ctx context.Context, req *mcp.CallToolRequest, args GreetInput) (*mcp.CallToolResult, GreetOutput, error) {
	greetResponse, err := s.greetService.Greet(ctx, greetdomain.GreetRequest{
		Name:     args.Name,
		Language: args.Language,
	})
	if err != nil {
		// 校验失败时返回带可读错误信息的失败结果，而不是断开连接。
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("参数错误: %v", err)},
			},
		}, GreetOutput{}, nil
	}

	var output = GreetOutput{
		Message:  greetResponse.Message,
		Language: greetResponse.Language,
		Name:     greetResponse.Name,
	}

	// 第二个返回值是结构化输出类型，SDK 会据其自动生成 OutputSchema 并填充
	// CallToolResult.StructuredContent；不要在此处手动设置该字段。
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: greetResponse.Message},
		},
	}, output, nil
}
