package oauthdomain

import (
	"fmt"
	"strings"
)

// Config 是 OAuth 授权服务器的配置。
//
// 授权服务器的标识与受保护资源是服务对外的身份属性，
// 与本域的协议契约（Token、AuthCode、Client）同属一处。
type Config struct {
	// Issuer 是授权服务器对外可达的地址，例如 https://mcp.example.com。
	//
	// 它是元数据里各端点的前缀，也用于生成 401 挑战中的
	// resource_metadata 地址。生产环境必须使用 HTTPS。
	Issuer string `yaml:"issuer"`

	// Resource 是受保护资源的标识，通常与 MCP 端点的地址一致。
	Resource string `yaml:"resource"`
}

// Validate 校验 OAuth 配置。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Issuer) == "" {
		return fmt.Errorf("oauth.issuer is required")
	}
	if strings.TrimSpace(c.Resource) == "" {
		return fmt.Errorf("oauth.resource is required")
	}
	return nil
}
