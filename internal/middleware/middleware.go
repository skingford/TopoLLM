package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/observability"
)

// RequestIDHeader 是请求 ID 的响应/请求头名称。
const RequestIDHeader = "X-Request-Id"

// contextKeyRequestID 是请求 ID 在 gin.Context 中的键。
const contextKeyRequestID = "request_id"

// RequestID 为每个请求注入唯一 ID（透传客户端已带的）。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(contextKeyRequestID, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// Logger 记录每个请求的方法、路径、状态码与耗时。
func Logger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("request",
			zap.String("request_id", c.GetString(contextKeyRequestID)),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
		)
	}
}

// Metrics 记录 Prometheus HTTP 指标（按路由模板，避免高基数）。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		observability.HTTPRequests.WithLabelValues(c.Request.Method, path, strconv.Itoa(c.Writer.Status())).Inc()
		observability.HTTPDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}

// Recovery 捕获 panic，返回 500 并记录上下文。
func Recovery(log *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Error("panic recovered",
			zap.String("request_id", c.GetString(contextKeyRequestID)),
			zap.Any("error", err),
		)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "internal server error", "type": "internal_error"},
		})
	})
}

// CORS 允许跨域（开发友好，生产应收紧来源）。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Auth 校验对外 API 令牌（Bearer）。Auth.Enabled=false 时放行。
func Auth(cfg config.AuthConfig) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(cfg.Tokens))
	for _, t := range cfg.Tokens {
		allowed[t] = struct{}{}
	}
	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}
		token := bearerToken(c.GetHeader("Authorization"))
		if _, ok := allowed[token]; !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "invalid api key", "type": "authentication_error"},
			})
			return
		}
		c.Next()
	}
}

// RateLimit 按 API 令牌（无则按客户端 IP）限流。Enabled=false 时放行。
// rdb 非空时用 Redis 固定窗口实现（跨实例一致）；否则用单机内存令牌桶。
func RateLimit(cfg config.RateLimitConfig, rdb *redis.Client) gin.HandlerFunc {
	local := newRateLimiter(cfg.RPM)
	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}
		key := bearerToken(c.GetHeader("Authorization"))
		if key == "" {
			key = c.ClientIP()
		}

		allowed := true
		if rdb != nil {
			allowed = redisAllow(c.Request.Context(), rdb, key, cfg.RPM)
		} else {
			allowed = local.allow(key)
		}
		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit_error"},
			})
			return
		}
		c.Next()
	}
}

// redisAllow 以"每分钟固定窗口"计数限流；Redis 出错时 fail-open。
func redisAllow(ctx context.Context, rdb *redis.Client, key string, rpm int) bool {
	window := time.Now().Unix() / 60
	rkey := fmt.Sprintf("topollm:rl:%s:%d", key, window)
	cnt, err := rdb.Incr(ctx, rkey).Result()
	if err != nil {
		return true // fail-open：限流组件不可用时不阻断业务
	}
	if cnt == 1 {
		rdb.Expire(ctx, rkey, 70*time.Second)
	}
	return cnt <= int64(rpm)
}

func bearerToken(h string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimPrefix(h, prefix)
	}
	return h
}

// ---- 内存令牌桶限流器（单机回退） ----

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu       sync.Mutex
	capacity float64
	refill   float64 // 每秒补充的令牌数
	buckets  map[string]*bucket
}

func newRateLimiter(rpm int) *rateLimiter {
	if rpm <= 0 {
		rpm = 60
	}
	return &rateLimiter{
		capacity: float64(rpm),
		refill:   float64(rpm) / 60.0,
		buckets:  make(map[string]*bucket),
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: rl.capacity, last: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.refill
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
