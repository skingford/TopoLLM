package task

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/dispatch"
)

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 404 仍算可达
	}))
	defer srv.Close()

	h := NewHealthChecker(dispatch.New(nil), time.Minute, zap.NewNop())
	if !h.probe(context.Background(), srv.URL) {
		t.Error("reachable server (even 404) should be healthy")
	}
	if h.probe(context.Background(), "http://127.0.0.1:1") {
		t.Error("unreachable address should be unhealthy")
	}
}

func TestCheckAll_FeedsBreaker(t *testing.T) {
	d := dispatch.New([]config.ChannelConfig{
		{Name: "dead", Adaptor: "openai", BaseURL: "http://127.0.0.1:1/v1", Models: []string{"m"}, Enabled: true},
	}, dispatch.WithBreaker(1, time.Hour))

	h := NewHealthChecker(d, time.Minute, zap.NewNop())
	h.checkAll(context.Background()) // 探测失败（阈值 1）-> 熔断打开

	if _, err := d.Select("m", nil); err == nil {
		t.Error("dead channel should be circuit-broken after failed health probe")
	}
}
