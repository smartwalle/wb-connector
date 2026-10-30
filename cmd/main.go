// Package main 是应用进程的入口：加载配置、准备运行环境、启动服务。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"wb-connector/internal/app"
	"wb-connector/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("应用运行失败", "error", err)
		os.Exit(1)
	}
}

// run 加载配置、准备运行环境并启动服务。
//
// 环境在这里创建并释放
func run() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("配置加载失败", slog.Any("error", err))
		return fmt.Errorf("load config: %w", err)
	}

	env, err := app.Prepare(cfg)
	if err != nil {
		slog.Error("运行环境准备失败", slog.Any("error", err))
		return fmt.Errorf("prepare environment: %w", err)
	}
	defer env.Clear()

	if err = app.Run(context.Background(), env); err != nil {
		slog.Error("应用启动失败", slog.Any("error", err))
		return err
	}
	return nil
}
