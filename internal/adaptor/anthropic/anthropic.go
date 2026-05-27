// Package anthropic 适配 Anthropic 原生 Messages 协议（/v1/messages）。
// 与统一的 OpenAI schema 双向转换：请求体、非流式响应、SSE 流式事件。
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/relay"
)

const (
	defaultMaxTokens = 4096
	anthropicVersion = "2023-06-01"
)

func init() {
	adaptor.Register("claude", func() adaptor.Adaptor { return &Adaptor{} })
}

// Adaptor 把统一 OpenAI schema 与 Anthropic Messages 协议互转。
type Adaptor struct{}

// Name 返回适配器标识。
func (*Adaptor) Name() string { return "claude" }

// Capabilities 声明该适配器支持的能力。
func (*Adaptor) Capabilities() adaptor.Capabilities {
	return adaptor.Capabilities{Chat: true, Stream: true, Tools: true, Vision: true, Reasoning: true}
}

// ---- 数据类型 ----

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens"`
	Temperature *float64     `json:"temperature"`
	TopP        *float64     `json:"top_p"`
	Stream      bool         `json:"stream"`
}

type antMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type antContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type antUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type antResponse struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	StopReason string            `json:"stop_reason"`
	Content    []antContentBlock `json:"content"`
	Usage      antUsage          `json:"usage"`
}

type oaiChoice struct {
	Index        int        `json:"index"`
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Model   string       `json:"model"`
	Choices []oaiChoice  `json:"choices"`
	Usage   *relay.Usage `json:"usage"`
}

// SetupRequest 把 OpenAI chat 请求转换为 Anthropic Messages 请求。
func (*Adaptor) SetupRequest(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	var oai oaiRequest
	if err := json.Unmarshal(in.Body, &oai); err != nil {
		return nil, err
	}

	ar := antRequest{
		Model:       oai.Model,
		MaxTokens:   oai.MaxTokens,
		Temperature: oai.Temperature,
		TopP:        oai.TopP,
		Stream:      oai.Stream,
	}
	if ar.MaxTokens <= 0 {
		ar.MaxTokens = defaultMaxTokens // Anthropic 要求 max_tokens 必填
	}

	var systemParts []string
	for _, m := range oai.Messages {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		ar.Messages = append(ar.Messages, antMessage{Role: m.Role, Content: m.Content})
	}
	ar.System = strings.Join(systemParts, "\n\n")

	payload, err := json.Marshal(ar)
	if err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(ch.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ch.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	if oai.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

// RelayResponse 把 Anthropic 响应转换为 OpenAI 格式写回客户端。
func (*Adaptor) RelayResponse(w http.ResponseWriter, resp *http.Response, stream bool) (*relay.Usage, error) {
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return passthrough(w, resp)
	}
	if stream {
		return convertStream(w, resp)
	}
	return convertJSON(w, resp)
}

func passthrough(w http.ResponseWriter, resp *http.Response) (*relay.Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
	return nil, nil
}

func convertJSON(w http.ResponseWriter, resp *http.Response) (*relay.Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ar antResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return nil, nil
	}

	var text strings.Builder
	for _, b := range ar.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	usage := &relay.Usage{
		PromptTokens:     ar.Usage.InputTokens,
		CompletionTokens: ar.Usage.OutputTokens,
		TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
	}
	out := oaiResponse{
		ID:     ar.ID,
		Object: "chat.completion",
		Model:  ar.Model,
		Choices: []oaiChoice{{
			Index:        0,
			Message:      oaiMessage{Role: "assistant", Content: text.String()},
			FinishReason: mapStopReason(ar.StopReason),
		}},
		Usage: usage,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
	return usage, nil
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Choices []streamChoice `json:"choices"`
}

func convertStream(w http.ResponseWriter, resp *http.Response) (*relay.Usage, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	const id = "chatcmpl-anthropic-stream"
	emit := func(choice streamChoice) {
		b, _ := json.Marshal(streamChunk{ID: id, Object: "chat.completion.chunk", Choices: []streamChoice{choice}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit(streamChoice{Index: 0, Delta: streamDelta{Role: "assistant"}}) // 起始角色帧

	usage := &relay.Usage{}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if bytes.HasPrefix(trimmed, []byte("data:")) {
				handleStreamEvent(bytes.TrimSpace(trimmed[len("data:"):]), usage, emit)
			}
		}
		if err != nil {
			break
		}
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage, nil
}

func handleStreamEvent(payload []byte, usage *relay.Usage, emit func(streamChoice)) {
	if len(payload) == 0 {
		return
	}
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Message struct {
			Usage antUsage `json:"usage"`
		} `json:"message"`
		Usage antUsage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}

	switch ev.Type {
	case "message_start":
		usage.PromptTokens = ev.Message.Usage.InputTokens
	case "content_block_delta":
		if ev.Delta.Text != "" {
			emit(streamChoice{Index: 0, Delta: streamDelta{Content: ev.Delta.Text}})
		}
	case "message_delta":
		if ev.Usage.OutputTokens > 0 {
			usage.CompletionTokens = ev.Usage.OutputTokens
		}
		if ev.Delta.StopReason != "" {
			reason := mapStopReason(ev.Delta.StopReason)
			emit(streamChoice{Index: 0, Delta: streamDelta{}, FinishReason: &reason})
		}
	}
}

func mapStopReason(s string) string {
	switch s {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
