package http

import (
	"fmt"
	"strings"
	"time"
)

// Config 是 HTTP 传输层的配置，也是配置文件里 http 段的定义。
//
// 配置结构、超时参数和校验规则都由传输层自己维护。
type Config struct {
	// Address 是监听地址，例如 127.0.0.1:8080。
	Address string `yaml:"address"`

	// ReadTimeout 是读取请求的超时时间。
	ReadTimeout time.Duration `yaml:"read_timeout"`

	// WriteTimeout 是写响应的超时时间。
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// IdleTimeout 是 keep-alive 空闲连接的超时时间。
	IdleTimeout time.Duration `yaml:"idle_timeout"`

	// OAuthIssuer 是授权服务器对外可达的地址，例如 https://mcp.example.com。
	//
	// 它是 OAuth 元数据里各端点的前缀，也用于生成 401 挑战中的
	// resource_metadata 地址。生产环境必须使用 HTTPS。
	OAuthIssuer string `yaml:"oauth_issuer"`

	// OAuthResource 是受保护资源的标识，通常与 MCP 端点的地址一致。
	OAuthResource string `yaml:"oauth_resource"`
}

// Validate 校验配置。错误信息带具体配置路径，便于定位问题。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return fmt.Errorf("http.address is required")
	}
	if c.ReadTimeout <= 0 {
		return fmt.Errorf("http.read_timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return fmt.Errorf("http.write_timeout must be positive")
	}
	if c.IdleTimeout <= 0 {
		return fmt.Errorf("http.idle_timeout must be positive")
	}
	if strings.TrimSpace(c.OAuthIssuer) == "" {
		return fmt.Errorf("http.oauth_issuer is required")
	}
	if strings.TrimSpace(c.OAuthResource) == "" {
		return fmt.Errorf("http.oauth_resource is required")
	}
	return nil
}
