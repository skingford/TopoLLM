// Package openaicompat 实现面向 OpenAI 兼容上游的通用适配器。
// 适用于 OpenAI、DeepSeek、通义千问(兼容模式)、智谱、Kimi、豆包、百川、讯飞、混元、Grok，
// 以及任意自定义的 OpenAI 兼容端点（自托管 vLLM/Ollama、第三方中转等）。
package openaicompat

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/relay"
)

func init() {
	adaptor.Register("openai", func() adaptor.Adaptor { return &Adaptor{} })
}

// Adaptor 是 OpenAI 兼容协议的通用适配器（近透传）。
type Adaptor struct{}

// Name 返回适配器标识。
func (*Adaptor) Name() string { return "openai" }

// Capabilities 声明该适配器支持的能力。
func (*Adaptor) Capabilities() adaptor.Capabilities {
	return adaptor.Capabilities{Chat: true, Stream: true, Tools: true, Vision: true, Embeddings: true}
}

// SetupRequest 构造面向上游的 chat/completions 请求。
func (*Adaptor) SetupRequest(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	return build(ctx, ch, "/chat/completions", in.Body, in.Stream)
}

// SetupEmbeddings 构造面向上游的 embeddings 请求（实现 adaptor.EmbeddingsAdaptor）。
func (*Adaptor) SetupEmbeddings(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	return build(ctx, ch, "/embeddings", in.Body, false)
}

func build(ctx context.Context, ch *adaptor.Channel, path string, body []byte, stream bool) (*http.Request, error) {
	url := strings.TrimSuffix(ch.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// RelayResponse 将上游响应近透传写回客户端，并尽力提取用量。
func (*Adaptor) RelayResponse(w http.ResponseWriter, resp *http.Response, stream bool) (*relay.Usage, error) {
	defer resp.Body.Close()
	if stream {
		return relay.WriteStream(w, resp)
	}
	return relay.WriteJSON(w, resp)
}
