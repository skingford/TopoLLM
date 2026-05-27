// Package gemini 适配 Google Gemini 原生协议（generateContent / streamGenerateContent）。
// 与统一的 OpenAI schema 双向转换：请求体（contents/parts）、响应、SSE 流式。
package gemini

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

func init() {
	adaptor.Register("gemini", func() adaptor.Adaptor { return &Adaptor{} })
}

// Adaptor 把统一 OpenAI schema 与 Gemini 原生协议互转。
type Adaptor struct{}

// Name 返回适配器标识。
func (*Adaptor) Name() string { return "gemini" }

// Capabilities 声明该适配器支持的能力。
func (*Adaptor) Capabilities() adaptor.Capabilities {
	return adaptor.Capabilities{Chat: true, Stream: true, Tools: true, Vision: true}
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
	Stream      bool         `json:"stream"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata geminiUsage       `json:"usageMetadata"`
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

// SetupRequest 把 OpenAI chat 请求转换为 Gemini generateContent 请求。
// 模型名置于 URL 路径；鉴权用 x-goog-api-key 头（避免密钥进 URL）。
func (*Adaptor) SetupRequest(ctx context.Context, in *adaptor.Request, ch *adaptor.Channel) (*http.Request, error) {
	var oai oaiRequest
	if err := json.Unmarshal(in.Body, &oai); err != nil {
		return nil, err
	}

	greq := geminiRequest{}
	var system []string
	for _, m := range oai.Messages {
		switch m.Role {
		case "system":
			system = append(system, m.Content)
		case "assistant":
			greq.Contents = append(greq.Contents, geminiContent{Role: "model", Parts: []geminiPart{{Text: m.Content}}})
		default:
			greq.Contents = append(greq.Contents, geminiContent{Role: "user", Parts: []geminiPart{{Text: m.Content}}})
		}
	}
	if len(system) > 0 {
		greq.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: strings.Join(system, "\n\n")}}}
	}
	if oai.MaxTokens > 0 || oai.Temperature != nil {
		greq.GenerationConfig = &geminiGenConfig{MaxOutputTokens: oai.MaxTokens, Temperature: oai.Temperature}
	}

	payload, err := json.Marshal(greq)
	if err != nil {
		return nil, err
	}

	method, query := "generateContent", ""
	if oai.Stream {
		method, query = "streamGenerateContent", "?alt=sse"
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:%s%s", strings.TrimSuffix(ch.BaseURL, "/"), oai.Model, method, query)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", ch.APIKey)
	return req, nil
}

// RelayResponse 把 Gemini 响应转换为 OpenAI 格式写回客户端。
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
	var gr geminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return nil, nil
	}

	var text strings.Builder
	finish := "stop"
	if len(gr.Candidates) > 0 {
		for _, p := range gr.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
		finish = mapFinishReason(gr.Candidates[0].FinishReason)
	}
	usage := &relay.Usage{
		PromptTokens:     gr.UsageMetadata.PromptTokenCount,
		CompletionTokens: gr.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      gr.UsageMetadata.TotalTokenCount,
	}
	out := oaiResponse{
		ID:     "chatcmpl-gemini",
		Object: "chat.completion",
		Choices: []oaiChoice{{
			Index:        0,
			Message:      oaiMessage{Role: "assistant", Content: text.String()},
			FinishReason: finish,
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

	const id = "chatcmpl-gemini-stream"
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
	return usage, nil
}

func handleStreamEvent(payload []byte, usage *relay.Usage, emit func(streamChoice)) {
	if len(payload) == 0 {
		return
	}
	var chunk geminiResponse
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return
	}

	if chunk.UsageMetadata.TotalTokenCount > 0 {
		usage.PromptTokens = chunk.UsageMetadata.PromptTokenCount
		usage.CompletionTokens = chunk.UsageMetadata.CandidatesTokenCount
		usage.TotalTokens = chunk.UsageMetadata.TotalTokenCount
	}
	if len(chunk.Candidates) == 0 {
		return
	}

	cand := chunk.Candidates[0]
	var text strings.Builder
	for _, p := range cand.Content.Parts {
		text.WriteString(p.Text)
	}
	if text.Len() > 0 {
		emit(streamChoice{Index: 0, Delta: streamDelta{Content: text.String()}})
	}
	if cand.FinishReason != "" {
		reason := mapFinishReason(cand.FinishReason)
		emit(streamChoice{Index: 0, Delta: streamDelta{}, FinishReason: &reason})
	}
}

func mapFinishReason(s string) string {
	switch s {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT":
		return "content_filter"
	default:
		return "stop"
	}
}
