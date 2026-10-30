package jsoncodec

// Response 是统一响应体。
type Response struct {
	// Code 是结果码，失败时说明失败原因。
	Code string `json:"code"`

	// Message 是结果描述，失败时说明失败原因。
	Message string `json:"message"`

	// Data 是业务数据。
	Data any `json:"data,omitempty"`
}
