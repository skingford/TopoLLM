// Command gateway 是 TopoLLM 大模型聚合网关的入口。
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/plugin"
	"github.com/kingford/TopoLLM/internal/server"
	"github.com/kingford/TopoLLM/internal/store"

	// 注册内置适配器与插件（通过 init() 自注册）。
	_ "github.com/kingford/TopoLLM/internal/adaptor/anthropic"
	_ "github.com/kingford/TopoLLM/internal/adaptor/azure"
	_ "github.com/kingford/TopoLLM/internal/adaptor/gemini"
	_ "github.com/kingford/TopoLLM/internal/adaptor/openaicompat"
	_ "github.com/kingford/TopoLLM/internal/plugin/builtin"
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

	disp := dispatch.New(cfg.Channels, dispatch.WithBreaker(
		cfg.CircuitBreaker.Threshold,
		time.Duration(cfg.CircuitBreaker.CooldownSeconds)*time.Second,
	))
	bill := billing.New(cfg.Billing, st.DB, log)
	chain, err := plugin.BuildChain(cfg.Plugins, log)
	if err != nil {
		log.Sugar().Fatalf("build plugin chain: %v", err)
	}
	log.Info("gateway initialized",
		zap.Int("channels", len(cfg.Channels)),
		zap.Bool("billing", cfg.Billing.Enabled),
		zap.Int("plugins", len(cfg.Plugins)),
	)

	srv := server.New(cfg, log, disp, bill, chain, st.Redis)
	if err := srv.Run(ctx); err != nil {
		log.Sugar().Fatalf("server: %v", err)
	}
}
