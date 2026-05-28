// Package server 封装 HTTP 服务：路由、中间件与优雅关闭。
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/admin"
	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/gateway"
	"github.com/kingford/TopoLLM/internal/middleware"
	"github.com/kingford/TopoLLM/internal/moderation"
	"github.com/kingford/TopoLLM/internal/plugin"
	"github.com/kingford/TopoLLM/internal/security"
)

// Server 封装 HTTP 服务与其依赖。
type Server struct {
	cfg  *config.Config
	log  *zap.Logger
	http *http.Server
}

// New 构建一个配置好路由与中间件的 Server。rdb 非空时用于分布式限流。
func New(cfg *config.Config, log *zap.Logger, d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, rdb *redis.Client) *Server {
	gin.SetMode(cfg.Server.Mode)
	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Logger(log),
		middleware.Metrics(),
		middleware.Recovery(log),
		middleware.CORS(),
	)

	s := &Server{
		cfg: cfg,
		log: log,
		http: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
			Handler:           engine,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	s.registerRoutes(engine, d, bill, chain, rdb)
	return s
}

func (s *Server) registerRoutes(e *gin.Engine, d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, rdb *redis.Client) {
	e.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	e.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// 对外 OpenAI 兼容端点：鉴权 + 限流。
	v1 := e.Group("/v1")
	v1.Use(middleware.Auth(s.cfg.Auth))
	v1.Use(middleware.RateLimit(s.cfg.RateLimit, rdb))
	v1.GET("/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []any{}})
	})
	moderator := moderation.New(s.cfg.OutputModeration.Enabled, s.cfg.OutputModeration.Words)
	v1.POST("/chat/completions", gateway.ChatCompletions(d, bill, chain, moderator, s.log))
	v1.POST("/embeddings", gateway.Embeddings(d, bill, chain, s.log))
	v1.POST("/images/generations", gateway.Images(d, bill, chain, s.log))
	v1.POST("/rerank", gateway.Rerank(d, bill, chain, s.log))

	// 运维管理 API（admin 令牌保护，含 SSRF 出口防护）。
	if s.cfg.Admin.Enabled {
		guard := security.NewEgressGuard(s.cfg.Admin.AllowPrivateUpstreams, s.cfg.Admin.AllowedUpstreamHosts)
		adminGroup := e.Group("/admin")
		adminGroup.Use(middleware.Auth(config.AuthConfig{Enabled: true, Tokens: []string{s.cfg.Admin.Token}}))
		admin.New(d, guard, s.log).Register(adminGroup)
	}
}

// Run 启动服务并阻塞，直至 ctx 取消后优雅关闭。
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("server listening", zap.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutting down server")
		timeout := time.Duration(s.cfg.Server.ShutdownTimeout) * time.Second
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}
