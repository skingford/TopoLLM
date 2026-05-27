package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kingford/TopoLLM/internal/config"
)

func newTestEngine(mw gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(mw)
	e.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	return e
}

func do(e *gin.Engine, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestAuth_Disabled(t *testing.T) {
	e := newTestEngine(Auth(config.AuthConfig{Enabled: false}))
	if rec := do(e, ""); rec.Code != http.StatusOK {
		t.Errorf("disabled auth should pass, got %d", rec.Code)
	}
}

func TestAuth_RejectsBadToken(t *testing.T) {
	e := newTestEngine(Auth(config.AuthConfig{Enabled: true, Tokens: []string{"sk-ok"}}))
	if rec := do(e, "sk-bad"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token = %d, want 401", rec.Code)
	}
	if rec := do(e, "sk-ok"); rec.Code != http.StatusOK {
		t.Errorf("good token = %d, want 200", rec.Code)
	}
}

func TestRateLimit_BlocksAfterBudget(t *testing.T) {
	e := newTestEngine(RateLimit(config.RateLimitConfig{Enabled: true, RPM: 1}))

	// 桶初始满（容量=rpm=1），首个请求放行。
	if rec := do(e, "k"); rec.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", rec.Code)
	}
	// 立即第二次：桶空且来不及补充，应 429。
	if rec := do(e, "k"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request = %d, want 429", rec.Code)
	}
}

func TestRateLimit_Disabled(t *testing.T) {
	e := newTestEngine(RateLimit(config.RateLimitConfig{Enabled: false, RPM: 1}))
	for i := 0; i < 5; i++ {
		if rec := do(e, "k"); rec.Code != http.StatusOK {
			t.Fatalf("disabled rate limit should pass, got %d", rec.Code)
		}
	}
}
