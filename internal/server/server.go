// Package server 封装 HTTP 服务：路由、中间件与优雅关闭。
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/middleware"
)

// Server 封装 HTTP 服务与其依赖。
type Server struct {
	cfg  *config.Config
	log  *zap.Logger
	http *http.Server
}

// New 构建一个配置好路由与中间件的 Server。
func New(cfg *config.Config, log *zap.Logger) *Server {
	gin.SetMode(cfg.Server.Mode)
	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Logger(log),
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
	s.registerRoutes(engine)
	return s
}

func (s *Server) registerRoutes(e *gin.Engine) {
	e.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 对外 OpenAI 兼容端点（Phase 1 起逐步实现）。
	v1 := e.Group("/v1")
	v1.GET("/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []any{}})
	})
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
