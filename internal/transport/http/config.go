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
	return nil
}
