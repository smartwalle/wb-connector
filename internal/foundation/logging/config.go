package logging

// Config 是日志配置，也是配置文件里 logging 段的定义。
//
// 当前没有可配置项，保留这一段是为了给以后增加日志级别、输出格式留位置。
type Config struct {
}

// Validate 校验配置。
func (c Config) Validate() error {
	return nil
}
