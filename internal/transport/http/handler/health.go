package handler

import (
	"time"

	"wb-connector/internal/transport/http/jsoncodec"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// HealthHandler 提供健康检查。
type HealthHandler struct {
}

// NewHealthHandler 创建健康检查 HealthHandler。
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// RegisterRoutes 声明本 HealthHandler 负责的路由。
func (h *HealthHandler) RegisterRoutes(r router.Router) {
	r.GET("/health", jsoncodec.Handler(h.Health))
}

// Health 返回服务状态。
func (h *HealthHandler) Health(c *rx.Context, _ struct{}) (*Health, error) {
	return &Health{Status: "ok", Time: time.Now().UTC().Format(time.RFC3339)}, nil
}

// Health 是健康检查的响应数据。
type Health struct {
	// Status 是服务状态。
	Status string `json:"status"`

	// Time 是服务当前时间。
	Time string `json:"time"`
}
