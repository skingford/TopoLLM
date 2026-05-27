// Package azure 适配 Azure OpenAI：URL 形如 /openai/deployments/{deployment}/{path}?api-version=，
// 鉴权用 api-key 头；请求/响应体与 OpenAI 兼容，故响应近透传。
package azure

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/relay"
)

const defaultAPIVersion = "2024-10-21"

func init() {
	adaptor.Register("azure", func() adaptor.Adaptor { return &Adaptor{} })
}

// Adaptor 是 Azure OpenAI 适配器。
type Adaptor struct{}

// Name 返回适配器标识。
func (*Adaptor) Name() string { return "azure" }

// Capabilities 声明该适配器支持的能力。
func (*Adaptor) Capabilities() adaptor.Capabilities {
	return adaptor.Capabilities{Chat: true, Stream: true, Tools: true, Vision: true, Embeddings: true}
}

// SetupRequest 构造 Azure chat/completions 请求。
func (*Adaptor) SetupRequest(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	return build(ctx, in, ch, "chat/completions")
}

// SetupEmbeddings 构造 Azure embeddings 请求（实现 adaptor.EmbeddingsAdaptor）。
func (*Adaptor) SetupEmbeddings(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	return build(ctx, in, ch, "embeddings")
}

func build(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel, path string) (*http.Request, error) {
	apiVersion := ch.Extra["api_version"]
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	deployment := ch.Extra["deployment"]
	if deployment == "" {
		deployment = in.Model // 默认 deployment 名与模型名一致
	}
	url := fmt.Sprintf("%s/openai/deployments/%s/%s?api-version=%s",
		strings.TrimSuffix(ch.BaseURL, "/"), deployment, path, apiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(in.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", ch.APIKey)
	if in.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// RelayResponse 近透传写回（Azure 响应与 OpenAI 兼容）。
func (*Adaptor) RelayResponse(w http.ResponseWriter, resp *http.Response, stream bool) (*relay.Usage, error) {
	defer resp.Body.Close()
	if stream {
		return relay.WriteStream(w, resp)
	}
	return relay.WriteJSON(w, resp)
}
