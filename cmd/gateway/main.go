// Command gateway 是 TopoLLM 大模型聚合网关的入口。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/server"
	"github.com/kingford/TopoLLM/internal/store"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	// 配置文件可选：不存在时退回默认值 + 环境变量。
	path := *configPath
	if _, err := os.Stat(path); err != nil {
		path = ""
	}

	cfg, err := config.Load(path)
	if err != nil {
		panic(err)
	}

	log, err := observability.NewLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg)
	if err != nil {
		log.Sugar().Fatalf("init store: %v", err)
	}
	defer func() { _ = st.Close() }()

	srv := server.New(cfg, log)
	if err := srv.Run(ctx); err != nil {
		log.Sugar().Fatalf("server: %v", err)
	}
}
