// Package openaicompat 实现面向 OpenAI 兼容上游的通用适配器。
// 适用于 OpenAI、DeepSeek、通义千问(兼容模式)、智谱、Kimi、豆包、百川、讯飞、混元，
// 以及任意自定义的 OpenAI 兼容端点（自托管 vLLM/Ollama、第三方中转等）。
package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	url := strings.TrimSuffix(ch.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(in.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ch.APIKey)
	if in.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// RelayResponse 将上游响应写回客户端，并尽力提取用量。
func (*Adaptor) RelayResponse(w http.ResponseWriter, resp *http.Response, stream bool) (*relay.Usage, error) {
	defer resp.Body.Close()
	if stream {
		return relayStream(w, resp)
	}
	return relayJSON(w, resp)
}

func relayJSON(w http.ResponseWriter, resp *http.Response) (*relay.Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	var parsed struct {
		Usage *relay.Usage `json:"usage"`
	}
	_ = json.Unmarshal(body, &parsed)
	return parsed.Usage, nil
}

func relayStream(w http.ResponseWriter, resp *http.Response) (*relay.Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	var usage *relay.Usage
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}
			if u := parseUsage(line); u != nil {
				usage = u
			}
		}
		if err != nil {
			if err == io.EOF {
				return usage, nil
			}
			return usage, err
		}
	}
}

// parseUsage 尝试从一行 SSE data 中解析 usage（OpenAI 在 include_usage 时于末帧返回）。
func parseUsage(line []byte) *relay.Usage {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return nil
	}
	payload := bytes.TrimSpace(trimmed[len("data:"):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}
	var parsed struct {
		Usage *relay.Usage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil
	}
	return parsed.Usage
}
