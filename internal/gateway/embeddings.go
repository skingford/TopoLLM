package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/plugin"
	"github.com/kingford/TopoLLM/internal/relay"
)

// Embeddings 返回 /v1/embeddings 处理器。
func Embeddings(d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, log *zap.Logger) gin.HandlerFunc {
	return passthrough(d, bill, chain, log, "embeddings")
}

// Images 返回 /v1/images/generations 处理器。
func Images(d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, log *zap.Logger) gin.HandlerFunc {
	return passthrough(d, bill, chain, log, "images/generations")
}

// Rerank 返回 /v1/rerank 处理器（如 SiliconFlow/Jina 风格的 OpenAI 兼容端点）。
func Rerank(d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, log *zap.Logger) gin.HandlerFunc {
	return passthrough(d, bill, chain, log, "rerank")
}

// passthrough 构造一个非流式透传处理器：把请求按 upstreamPath 转发到支持 PathAdaptor 的渠道。
// 复用插件前置、三阶段计费与故障转移逻辑。
func passthrough(d *dispatch.Dispatcher, bill *billing.Service, chain *plugin.Chain, log *zap.Logger, upstreamPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "failed to read request body")
			return
		}

		var head struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &head); err != nil || head.Model == "" {
			writeError(c, http.StatusBadRequest, "invalid request: missing model")
			return
		}

		token := bearerToken(c.GetHeader("Authorization"))
		reqID := c.GetString("request_id")

		pctx := &plugin.Context{RequestID: reqID, Token: token, Model: head.Model, Body: body, Log: log}
		if res := chain.OnRequest(pctx); res.Action == plugin.ActionReject {
			writeError(c, res.Status, res.Message)
			return
		}
		body = pctx.Body

		reserved, err := bill.Reserve(token, head.Model, len(body)/charsPerToken, 0)
		if err != nil {
			writeError(c, http.StatusPaymentRequired, "insufficient quota for model: "+head.Model)
			return
		}
		settled := false
		settle := func(channel string, usage *relay.Usage) {
			bill.Settle(token, head.Model, reqID, channel, reserved, usage)
			settled = true
		}
		defer func() {
			if !settled {
				bill.Refund(token, reserved)
			}
		}()

		in := &adaptor.Request{Model: head.Model, Body: body}
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
				excluded[ch.Name] = true
				continue
			}
			pa, ok := ad.(adaptor.PathAdaptor)
			if !ok {
				writeError(c, http.StatusBadRequest, "model does not support /"+upstreamPath+": "+head.Model)
				return
			}

			req, err := pa.SetupPath(c.Request.Context(), in, ch, upstreamPath)
			if err != nil {
				excluded[ch.Name] = true
				continue
			}

			resp, err := upstreamClient.Do(req)
			if err != nil {
				log.Warn("passthrough upstream failed", zap.String("channel", ch.Name), zap.String("path", upstreamPath), zap.Error(err))
				d.RecordResult(ch.Name, false)
				excluded[ch.Name] = true
				continue
			}
			if resp.StatusCode >= http.StatusInternalServerError {
				resp.Body.Close()
				d.RecordResult(ch.Name, false)
				excluded[ch.Name] = true
				continue
			}

			usage, err := ad.RelayResponse(c.Writer, resp, false)
			if err != nil {
				observability.RelayRequests.WithLabelValues(ch.Name, head.Model, "error").Inc()
				settle(ch.Name, usage)
				return
			}
			d.RecordResult(ch.Name, true)
			observability.RelayRequests.WithLabelValues(ch.Name, head.Model, "success").Inc()
			recordTokens(ch.Name, head.Model, usage)
			settle(ch.Name, usage)
			return
		}

		observability.RelayRequests.WithLabelValues("none", head.Model, "failed").Inc()
		writeError(c, http.StatusBadGateway, "all upstream channels failed for model: "+head.Model)
	}
}
