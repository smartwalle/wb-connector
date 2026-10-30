package greetdomain

import "context"

// GreetRequest 是"生成问候语"用例的输入契约。
type GreetRequest struct {
	// Name 是要问候的对象姓名，必填。
	Name string `json:"name"`

	// Language 是问候语语言，可选，缺省为中文（zh），也可传 en。
	Language string `json:"language,omitempty"`
}

// GreetResponse 是问候语实体。
type GreetResponse struct {
	// Message 是最终展示给用户的问候语文本。
	Message string `json:"message"`

	// Language 是问候语使用的语言，zh 或 en。
	Language string `json:"language"`

	// Name 是归一化（去除首尾空白）后的姓名。
	Name string `json:"name"`
}

// Service 是问候模块的业务契约，每个方法是一个业务用例。
//
// 实现由 internal/service 提供，在应用组装层注入。
//
// 契约不感知传输方式：同一份实现既可以被 MCP 工具调用，
// 也可以被普通 HTTP 接口调用。
type Service interface {
	// Greet 生成一句问候语。
	//
	// 姓名为空或语言不受支持时返回领域错误。
	Greet(ctx context.Context, req GreetRequest) (*GreetResponse, error)
}
