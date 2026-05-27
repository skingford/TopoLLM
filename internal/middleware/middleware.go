package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
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
