package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	_ "github.com/kingford/TopoLLM/internal/adaptor/openaicompat" // 注册 "openai" 适配器
	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
)

func newEngine(d *dispatch.Dispatcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.POST("/v1/chat/completions", ChatCompletions(d, zap.NewNop()))
	return e
}

func post(e *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestChatCompletions_NonStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
	}))
	defer upstream.Close()

	d := dispatch.New([]config.ChannelConfig{
		{Name: "test", Adaptor: "openai", BaseURL: upstream.URL, Models: []string{"gpt"}, Weight: 1, Enabled: true},
	})
	rec := post(newEngine(d), `{"model":"gpt","messages":[{"role":"user","content":"hello"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object = %v", resp["object"])
	}
}

func TestChatCompletions_Stream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	d := dispatch.New([]config.ChannelConfig{
		{Name: "test", Adaptor: "openai", BaseURL: upstream.URL, Models: []string{"gpt"}, Weight: 1, Enabled: true},
	})
	rec := post(newEngine(d), `{"model":"gpt","stream":true,"messages":[]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Errorf("stream body missing [DONE]: %s", rec.Body.String())
	}
}

func TestChatCompletions_Failover(t *testing.T) {
	var firstHit, secondHit bool
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHit = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHit = true
		_, _ = w.Write([]byte(`{"object":"chat.completion"}`))
	}))
	defer good.Close()

	d := dispatch.New([]config.ChannelConfig{
		{Name: "bad", Adaptor: "openai", BaseURL: bad.URL, Models: []string{"gpt"}, Weight: 1, Priority: 10, Enabled: true},
		{Name: "good", Adaptor: "openai", BaseURL: good.URL, Models: []string{"gpt"}, Weight: 1, Priority: 5, Enabled: true},
	})
	rec := post(newEngine(d), `{"model":"gpt","messages":[]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover; body=%s", rec.Code, rec.Body.String())
	}
	if !firstHit || !secondHit {
		t.Errorf("failover did not hit both upstreams: first=%v second=%v", firstHit, secondHit)
	}
}

func TestChatCompletions_UnknownModel(t *testing.T) {
	rec := post(newEngine(dispatch.New(nil)), `{"model":"nope","messages":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestChatCompletions_BadRequest(t *testing.T) {
	rec := post(newEngine(dispatch.New(nil)), `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
