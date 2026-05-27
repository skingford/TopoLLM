// Package gateway 实现对外 OpenAI 兼容端点的请求编排：
// 解析 → 选渠道 → 适配器转换 → 转发上游 → 故障转移 → 写回客户端。
package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/relay"
)

const maxFailoverAttempts = 3

// upstreamClient 不设整体 Timeout，以支持长连接流式；取消由请求 context 控制。
var upstreamClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
	},
}

// ChatCompletions 返回 /v1/chat/completions 的处理器。
func ChatCompletions(d *dispatch.Dispatcher, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "failed to read request body")
			return
		}

		var head struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.Unmarshal(body, &head); err != nil || head.Model == "" {
			writeError(c, http.StatusBadRequest, "invalid request: missing model")
			return
		}

		in := &adaptor.Request{Model: head.Model, Stream: head.Stream, Body: body}
		excluded := make(map[string]bool)

		for attempt := 0; attempt < maxFailoverAttempts; attempt++ {
			ch, err := d.Select(head.Model, excluded)
			if err != nil {
				if attempt == 0 {
					writeError(c, http.StatusNotFound, "no channel available for model: "+head.Model)
					return
				}
				break
			}

			ad, ok := adaptor.Get(ch.Adaptor)
			if !ok {
				log.Warn("unknown adaptor", zap.String("channel", ch.Name), zap.String("adaptor", ch.Adaptor))
				excluded[ch.Name] = true
				continue
			}

			req, err := ad.SetupRequest(c.Request.Context(), in, ch)
			if err != nil {
				excluded[ch.Name] = true
				continue
			}

			start := time.Now()
			resp, err := upstreamClient.Do(req)
			if err != nil {
				log.Warn("upstream request failed", zap.String("channel", ch.Name), zap.Error(err))
				d.RecordResult(ch.Name, false)
				excluded[ch.Name] = true
				continue
			}

			// 5xx 视为渠道故障，转移到下一渠道。
			if resp.StatusCode >= http.StatusInternalServerError {
				resp.Body.Close()
				log.Warn("upstream 5xx, failing over",
					zap.String("channel", ch.Name), zap.Int("status", resp.StatusCode))
				d.RecordResult(ch.Name, false)
				excluded[ch.Name] = true
				continue
			}

			usage, err := ad.RelayResponse(c.Writer, resp, head.Stream)
			if err != nil {
				log.Error("relay response failed", zap.String("channel", ch.Name), zap.Error(err))
				observability.RelayRequests.WithLabelValues(ch.Name, head.Model, "error").Inc()
				return
			}
			d.RecordResult(ch.Name, true)
			logUsage(log, c, ch, head.Model, usage, time.Since(start))
			observability.RelayRequests.WithLabelValues(ch.Name, head.Model, "success").Inc()
			recordTokens(ch.Name, head.Model, usage)
			return
		}

		observability.RelayRequests.WithLabelValues("none", head.Model, "failed").Inc()
		writeError(c, http.StatusBadGateway, "all upstream channels failed for model: "+head.Model)
	}
}

func recordTokens(channel, model string, usage *relay.Usage) {
	if usage == nil {
		return
	}
	observability.RelayTokens.WithLabelValues(channel, model, "prompt").Add(float64(usage.PromptTokens))
	observability.RelayTokens.WithLabelValues(channel, model, "completion").Add(float64(usage.CompletionTokens))
}

func logUsage(log *zap.Logger, c *gin.Context, ch *adaptor.Channel, model string, usage *relay.Usage, latency time.Duration) {
	fields := []zap.Field{
		zap.String("request_id", c.GetString("request_id")),
		zap.String("channel", ch.Name),
		zap.String("model", model),
		zap.Duration("latency", latency),
	}
	if usage != nil {
		fields = append(fields,
			zap.Int("prompt_tokens", usage.PromptTokens),
			zap.Int("completion_tokens", usage.CompletionTokens),
			zap.Int("total_tokens", usage.TotalTokens),
		)
	}
	log.Info("relay", fields...)
}

func writeError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": "gateway_error"}})
}
