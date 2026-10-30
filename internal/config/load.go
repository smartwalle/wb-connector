package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load 读取并解析 YAML 配置文件。
//
// 加载步骤：读取文件 -> 严格解码 YAML -> 校验。
func Load(filepath string) (*Config, error) {
	var file, err = os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", filepath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	var decoder = yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var cfg *Config
	if err = decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	if err = cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", filepath, err)
	}
	return cfg, nil
}
