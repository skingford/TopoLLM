package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kingford/TopoLLM/internal/adaptor"
)

func TestSetupRequest_TransformsToAnthropic(t *testing.T) {
	in := &adaptor.Request{
		Model: "claude-3-5-sonnet",
		Body:  []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]}`),
	}
	ch := &adaptor.Channel{Name: "c", BaseURL: "https://api.anthropic.com", APIKey: "k"}

	req, err := (&Adaptor{}).SetupRequest(context.Background(), in, ch)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://api.anthropic.com/v1/messages" {
		t.Errorf("url = %s", req.URL.String())
	}
	if req.Header.Get("x-api-key") != "k" {
		t.Error("missing x-api-key header")
	}
	if req.Header.Get("anthropic-version") == "" {
		t.Error("missing anthropic-version header")
	}

	body, _ := io.ReadAll(req.Body)
	var ar antRequest
	if err := json.Unmarshal(body, &ar); err != nil {
		t.Fatal(err)
	}
	if ar.System != "be brief" {
		t.Errorf("system = %q, want 'be brief'", ar.System)
	}
	if len(ar.Messages) != 1 || ar.Messages[0].Role != "user" {
		t.Errorf("messages = %+v (system should be extracted)", ar.Messages)
	}
	if ar.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want default %d", ar.MaxTokens, defaultMaxTokens)
	}
}

func makeResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestRelayResponse_NonStream(t *testing.T) {
	antJSON := `{"id":"msg_1","model":"claude-3-5-sonnet","stop_reason":"end_turn","content":[{"type":"text","text":"hello world"}],"usage":{"input_tokens":10,"output_tokens":3}}`
	rec := httptest.NewRecorder()
	usage, err := (&Adaptor{}).RelayResponse(rec, makeResp(http.StatusOK, antJSON), false)
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.TotalTokens != 13 {
		t.Fatalf("usage = %+v, want total 13", usage)
	}

	var out oaiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "chat.completion" {
		t.Errorf("object = %s", out.Object)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "hello world" {
		t.Errorf("choices = %+v", out.Choices)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %s, want stop", out.Choices[0].FinishReason)
	}
}

func TestRelayResponse_Stream(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":7,"output_tokens":0}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		``,
	}, "\n")

	rec := httptest.NewRecorder()
	usage, err := (&Adaptor{}).RelayResponse(rec, makeResp(http.StatusOK, sse), true)
	if err != nil {
		t.Fatal(err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"content":"Hel"`) || !strings.Contains(out, `"content":"lo"`) {
		t.Errorf("stream missing content deltas: %s", out)
	}
	if !strings.Contains(out, "[DONE]") {
		t.Error("stream missing [DONE]")
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 2 || usage.TotalTokens != 9 {
		t.Errorf("usage = %+v, want 7/2/9", usage)
	}
}

func TestRelayResponse_ErrorPassthrough(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := (&Adaptor{}).RelayResponse(rec, makeResp(http.StatusBadRequest, `{"type":"error"}`), false); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := map[string]string{"end_turn": "stop", "max_tokens": "length", "tool_use": "tool_calls", "weird": "stop"}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}
