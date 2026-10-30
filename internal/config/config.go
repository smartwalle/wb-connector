// Package config 负责配置文件的加载与校验。
//
// 配置结构由各功能模块自己维护：传输层维护 http 段，平台层维护 database 与 logging 段，
// 这里只负责把它们组合成配置文件的结构，并完成解码与校验。
package config

import (
	"fmt"
	"strings"
	"time"

	"wb-connector/internal/foundation/logging"
	"wb-connector/internal/transport/http"
)

// Config 是配置文件的结构。
type Config struct {
	// App 是应用元信息。
	App AppConfig `yaml:"app"`

	// HTTP 是 HTTP 服务配置，由传输层维护。
	HTTP http.Config `yaml:"http"`

	// Logging 是日志配置，由日志模块维护。
	Logging logging.Config `yaml:"logging"`
}

// AppConfig 是应用元信息。
type AppConfig struct {
	// Name 是应用名。
	Name string `yaml:"name"`

	// Environment 是运行环境，例如 development、test、production。
	Environment string `yaml:"environment"`

	// ShutdownTimeout 是优雅关闭的最长等待时间。
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// Validate 校验配置：应用自身的字段在这里校验，各模块的配置由模块自己校验。
func (c Config) Validate() error {
	if strings.TrimSpace(c.App.Name) == "" {
		return fmt.Errorf("app.name is required")
	}
	if strings.TrimSpace(c.App.Environment) == "" {
		return fmt.Errorf("app.environment is required")
	}
	if c.App.ShutdownTimeout <= 0 {
		return fmt.Errorf("app.shutdown_timeout must be positive")
	}
	if err := c.Logging.Validate(); err != nil {
		return err
	}
	if err := c.HTTP.Validate(); err != nil {
		return err
	}
	return nil
}
