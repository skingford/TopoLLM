// Package builtin 提供内置插件（敏感词拦截、审计日志、PII 脱敏），通过 init 注册到 plugin 注册表。
// 在入口处空导入本包即可启用：_ "github.com/kingford/TopoLLM/internal/plugin/builtin"
package builtin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/plugin"
)

func init() {
	plugin.Register("sensitive_words", newSensitiveWords)
	plugin.Register("audit", newAudit)
	plugin.Register("pii_mask", newPIIMask)
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

// ---- pii_mask：把消息内容中的 PII 替换为占位符 ----

type piiPattern struct {
	re   *regexp.Regexp
	repl string
}

type piiMask struct {
	patterns []piiPattern
}

// defaultPIIPatterns 是内置默认模式。可通过 options.custom 追加自定义模式：
//   options.custom: [{pattern: "<regex>", replace: "<label>"}, ...]
var defaultPIIPatterns = []piiPattern{
	{regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), "[EMAIL]"},
	{regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), "[SSN]"},
	{regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`), "[CARD]"},
	{regexp.MustCompile(`\b\d{3}[\-. ]\d{3}[\-. ]\d{4}\b`), "[PHONE]"},
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), "[IP]"},
}

func newPIIMask(opts map[string]any) (plugin.Plugin, error) {
	patterns := append([]piiPattern{}, defaultPIIPatterns...)
	if raw, ok := opts["custom"].([]any); ok {
		for _, c := range raw {
			m, ok := c.(map[string]any)
			if !ok {
				continue
			}
			pat, _ := m["pattern"].(string)
			repl, _ := m["replace"].(string)
			if pat == "" {
				continue
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				continue
			}
			if repl == "" {
				repl = "[REDACTED]"
			}
			patterns = append(patterns, piiPattern{re: re, repl: repl})
		}
	}
	return &piiMask{patterns: patterns}, nil
}

func (*piiMask) Name() string { return "pii_mask" }

// OnRequest 解析 messages[].content，逐条做正则替换；命中即改写 ctx.Body。
func (p *piiMask) OnRequest(ctx *plugin.Context) plugin.Result {
	var req map[string]any
	if err := json.Unmarshal(ctx.Body, &req); err != nil {
		return plugin.Continue()
	}
	msgs, ok := req["messages"].([]any)
	if !ok {
		return plugin.Continue()
	}

	modified := false
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		content, ok := mm["content"].(string)
		if !ok {
			continue
		}
		masked := content
		for _, pat := range p.patterns {
			masked = pat.re.ReplaceAllString(masked, pat.repl)
		}
		if masked != content {
			mm["content"] = masked
			modified = true
		}
	}

	if modified {
		if newBody, err := json.Marshal(req); err == nil {
			ctx.Body = newBody
		}
	}
	return plugin.Continue()
}
