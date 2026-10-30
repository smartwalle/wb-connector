package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
	"wb-connector/internal/domain/greetdomain"
	"wb-connector/internal/domain/metadata"
	"wb-connector/internal/foundation/errors"
)

// maxNameLength 是姓名的最大字符数。
const maxNameLength = 64

// GreetService 实现问候模块的业务用例。
type GreetService struct {
}

// NewGreetService 创建 GreetService。
func NewGreetService() *GreetService {
	return &GreetService{}
}

// Greet 生成一句问候语。
//
// 姓名为空返回 greetdomain.ErrEmptyName；语言归一化后既不是 zh 也不是 en 时返回
// greetdomain.ErrUnsupportedLanguage。
func (s *GreetService) Greet(ctx context.Context, req greetdomain.GreetRequest) (*greetdomain.GreetResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.MissingArgument("name is required")
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return nil, fmt.Errorf("name exceeds %d characters", maxNameLength)
	}

	language := normalizeLanguage(req.Language)
	if language == "" {
		return nil, errors.InvalidArgument("language must be zh or en")
	}

	var token = metadata.TokenFrom(ctx)

	var message string
	if language == "en" {
		message = fmt.Sprintf("Hello, %s! Welcome to WorkBuddy. I'm %s", name, token.UserID)
	} else {
		message = fmt.Sprintf("你好，%s！欢迎使用 WorkBuddy, 我是 %s", name, token.UserID)
	}

	return &greetdomain.GreetResponse{
		Message:  message,
		Language: language,
		Name:     name,
	}, nil
}

// normalizeLanguage 归一化语言取值。
//
// 空值按中文处理；大小写与地区后缀（en-US、zh_CN）都归一到 en 或 zh；
// 其它取值返回空串表示不受支持。
func normalizeLanguage(language string) string {
	var value = strings.ToLower(strings.TrimSpace(language))
	if value == "" {
		return "zh"
	}
	if idx := strings.IndexAny(value, "-_"); idx >= 0 {
		value = value[:idx]
	}
	switch value {
	case "zh", "en":
		return value
	default:
		return ""
	}
}
