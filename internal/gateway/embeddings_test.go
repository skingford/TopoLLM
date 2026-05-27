package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	_ "github.com/kingford/TopoLLM/internal/adaptor/anthropic" // 注册 "claude"（不支持 embeddings）
	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/plugin"
)

func embEngine(d *dispatch.Dispatcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	bill := billing.New(config.BillingConfig{}, nil, zap.NewNop())
	chain, _ := plugin.BuildChain(nil, zap.NewNop())
	e := gin.New()
	e.POST("/v1/embeddings", Embeddings(d, bill, chain, zap.NewNop()))
	return e
}

func postEmb(e *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestEmbeddings_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Errorf("expected /embeddings path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`))
	}))
	defer upstream.Close()

	d := dispatch.New([]config.ChannelConfig{
		{Name: "e", Adaptor: "openai", BaseURL: upstream.URL, Models: []string{"emb"}, Enabled: true},
	})
	rec := postEmb(embEngine(d), `{"model":"emb","input":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "embedding") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestEmbeddings_Unsupported(t *testing.T) {
	// claude 适配器未实现 EmbeddingsAdaptor，应返回 400。
	d := dispatch.New([]config.ChannelConfig{
		{Name: "c", Adaptor: "claude", BaseURL: "http://127.0.0.1:1", Models: []string{"m"}, Enabled: true},
	})
	rec := postEmb(embEngine(d), `{"model":"m","input":"hi"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (embeddings unsupported)", rec.Code)
	}
}

func TestEmbeddings_UnknownModel(t *testing.T) {
	rec := postEmb(embEngine(dispatch.New(nil)), `{"model":"nope","input":"hi"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
