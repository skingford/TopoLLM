package gemini

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

func TestSetupRequest_NonStream(t *testing.T) {
	in := &adaptor.Request{
		Model: "gemini-2.5-flash",
		Body:  []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"system","content":"sys"},{"role":"assistant","content":"prev"},{"role":"user","content":"hi"}]}`),
	}
	ch := &adaptor.Channel{Name: "g", BaseURL: "https://generativelanguage.googleapis.com", APIKey: "AIza-key"}

	req, err := (&Adaptor{}).SetupRequest(context.Background(), in, ch)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
	if req.URL.String() != wantURL {
		t.Errorf("url = %s, want %s", req.URL.String(), wantURL)
	}
	if req.Header.Get("x-goog-api-key") != "AIza-key" {
		t.Error("missing x-goog-api-key header")
	}

	body, _ := io.ReadAll(req.Body)
	var gr geminiRequest
	if err := json.Unmarshal(body, &gr); err != nil {
		t.Fatal(err)
	}
	if gr.SystemInstruction == nil || gr.SystemInstruction.Parts[0].Text != "sys" {
		t.Errorf("systemInstruction = %+v", gr.SystemInstruction)
	}
	if len(gr.Contents) != 2 {
		t.Fatalf("contents len = %d, want 2", len(gr.Contents))
	}
	if gr.Contents[0].Role != "model" { // assistant -> model
		t.Errorf("assistant role = %q, want model", gr.Contents[0].Role)
	}
	if gr.Contents[1].Role != "user" {
		t.Errorf("user role = %q, want user", gr.Contents[1].Role)
	}
}

func TestSetupRequest_StreamURL(t *testing.T) {
	in := &adaptor.Request{
		Model: "gemini-2.5-pro",
		Body:  []byte(`{"model":"gemini-2.5-pro","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
	}
	ch := &adaptor.Channel{BaseURL: "https://generativelanguage.googleapis.com", APIKey: "k"}
	req, err := (&Adaptor{}).SetupRequest(context.Background(), in, ch)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.URL.String(), ":streamGenerateContent") || !strings.Contains(req.URL.RawQuery, "alt=sse") {
		t.Errorf("stream url = %s", req.URL.String())
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
	geminiJSON := `{"candidates":[{"content":{"parts":[{"text":"hello"},{"text":" world"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`
	rec := httptest.NewRecorder()
	usage, err := (&Adaptor{}).RelayResponse(rec, makeResp(http.StatusOK, geminiJSON), false)
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.TotalTokens != 6 {
		t.Fatalf("usage = %+v, want total 6", usage)
	}
	var out oaiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
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
		`data: {"candidates":[{"content":{"parts":[{"text":"Hel"}],"role":"model"}}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"text":"lo"}],"role":"model"}}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`,
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
	if usage.TotalTokens != 5 {
		t.Errorf("usage total = %d, want 5", usage.TotalTokens)
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := map[string]string{"STOP": "stop", "MAX_TOKENS": "length", "SAFETY": "content_filter", "X": "stop"}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}
