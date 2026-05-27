package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/dispatch"
	"github.com/kingford/TopoLLM/internal/security"
)

func setup(allowPrivate bool) (*gin.Engine, *dispatch.Dispatcher) {
	gin.SetMode(gin.TestMode)
	d := dispatch.New(nil)
	api := New(d, security.NewEgressGuard(allowPrivate, nil), zap.NewNop())
	e := gin.New()
	api.Register(e.Group("/admin"))
	return e, d
}

func do(e *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, r)
	return rec
}

func TestAddAndList(t *testing.T) {
	e, d := setup(true) // allowPrivate 以便使用内网测试地址
	rec := do(e, http.MethodPost, "/admin/channels",
		`{"name":"c1","adaptor":"openai","base_url":"http://127.0.0.1:1/v1","models":["gpt"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(d.List()) != 1 {
		t.Errorf("dispatcher channels = %d, want 1", len(d.List()))
	}
	if rec := do(e, http.MethodGet, "/admin/channels", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "c1") {
		t.Errorf("list = %s", rec.Body.String())
	}
}

func TestAddBlockedBySSRF(t *testing.T) {
	e, _ := setup(false) // 不允许内网
	rec := do(e, http.MethodPost, "/admin/channels",
		`{"name":"evil","adaptor":"openai","base_url":"http://127.0.0.1/v1","models":["x"]}`)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (SSRF guard)", rec.Code)
	}
}

func TestDelete(t *testing.T) {
	e, d := setup(true)
	do(e, http.MethodPost, "/admin/channels",
		`{"name":"c1","adaptor":"openai","base_url":"http://127.0.0.1:1/v1","models":["gpt"]}`)
	rec := do(e, http.MethodDelete, "/admin/channels/c1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", rec.Code)
	}
	if len(d.List()) != 0 {
		t.Errorf("channels after delete = %d, want 0", len(d.List()))
	}
	if rec := do(e, http.MethodDelete, "/admin/channels/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing status = %d, want 404", rec.Code)
	}
}
