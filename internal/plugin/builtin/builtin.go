// Package builtin 提供内置插件（敏感词拦截、审计日志），通过 init 注册到 plugin 注册表。
// 在入口处空导入本包即可启用：_ "github.com/kingford/TopoLLM/internal/plugin/builtin"
package builtin

import (
	"bytes"
	"net/http"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/plugin"
)

func init() {
	plugin.Register("sensitive_words", newSensitiveWords)
	plugin.Register("audit", newAudit)
}

// ---- sensitive_words：命中配置词则短路拒绝 ----

type sensitiveWords struct {
	words [][]byte
}

func newSensitiveWords(opts map[string]any) (plugin.Plugin, error) {
	sw := &sensitiveWords{}
	if raw, ok := opts["words"].([]any); ok {
		for _, w := range raw {
			if s, ok := w.(string); ok && s != "" {
				sw.words = append(sw.words, []byte(s))
			}
		}
	}
	return sw, nil
}

func (*sensitiveWords) Name() string { return "sensitive_words" }

func (s *sensitiveWords) OnRequest(ctx *plugin.Context) plugin.Result {
	for _, w := range s.words {
		if bytes.Contains(ctx.Body, w) {
			return plugin.Reject(http.StatusBadRequest, "request blocked by content policy")
		}
	}
	return plugin.Continue()
}

// ---- audit：记录请求审计日志 ----

type audit struct{}

func newAudit(map[string]any) (plugin.Plugin, error) { return &audit{}, nil }

func (*audit) Name() string { return "audit" }

func (*audit) OnRequest(ctx *plugin.Context) plugin.Result {
	if ctx.Log != nil {
		ctx.Log.Info("audit",
			zap.String("request_id", ctx.RequestID),
			zap.String("model", ctx.Model),
			zap.Bool("stream", ctx.Stream),
		)
	}
	return plugin.Continue()
}
