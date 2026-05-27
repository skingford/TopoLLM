// Package gateway 实现对外 OpenAI 兼容端点的请求编排：
// 解析 → 计费预扣 → 选渠道 → 适配器转换 → 转发上游 → 故障转移 → 结算/退款 → 写回客户端。
package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/relay"
)

const (
	maxFailoverAttempts = 3
	charsPerToken       = 4 // 粗略的 prompt token 估算（约 4 字符/token）
)

// upstreamClient 不设整体 Timeout，以支持长连接流式；取消由请求 context 控制。
var upstreamClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
	},
}

// ChatCompletions 返回 /v1/chat/completions 的处理器。
func ChatCompletions(d *dispatch.Dispatcher, bill *billing.Service, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "failed to read request body")
			return
		}

		var head struct {
			Model     string `json:"model"`
			Stream    bool   `json:"stream"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.Unmarshal(body, &head); err != nil || head.Model == "" {
			writeError(c, http.StatusBadRequest, "invalid request: missing model")
			return
		}

		// 计费三阶段（1/3）：预扣估算费用。
		token := bearerToken(c.GetHeader("Authorization"))
		reserved, err := bill.Reserve(token, head.Model, len(body)/charsPerToken, head.MaxTokens)
		if err != nil {
			writeError(c, http.StatusPaymentRequired, "insufficient quota for model: "+head.Model)
			return
		}
		reqID := c.GetString("request_id")
		settled := false
		settle := func(channel string, usage *relay.Usage) {
			bill.Settle(token, head.Model, reqID, channel, reserved, usage) // 阶段 2/3：结算
			settled = true
		}
		defer func() {
			if !settled {
				bill.Refund(token, reserved) // 阶段 3/3：失败全额退款
			}
		}()

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
				settle(ch.Name, usage)
				return
			}
			d.RecordResult(ch.Name, true)
			logUsage(log, c, ch, head.Model, usage, time.Since(start))
			observability.RelayRequests.WithLabelValues(ch.Name, head.Model, "success").Inc()
			recordTokens(ch.Name, head.Model, usage)
			settle(ch.Name, usage)
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

func bearerToken(h string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimPrefix(h, prefix)
	}
	return h
}

func writeError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"message": msg, "type": "gateway_error"}})
}
