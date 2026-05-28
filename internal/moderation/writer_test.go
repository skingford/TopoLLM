package moderation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStream_BlocksOnBannedWord(t *testing.T) {
	rec := httptest.NewRecorder()
	w := New(true, []string{"forbidden"}).Wrap(rec, true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"hello "}}]}` + "\n"))
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"forbidden"}}]}` + "\n")) // 触发
	w.Write([]byte(`data: {"choices":[{"delta":{"content":" more"}}]}` + "\n"))     // 应被丢弃
	w.Write([]byte("data: [DONE]\n"))

	out := rec.Body.String()
	if !strings.Contains(out, "hello ") {
		t.Error("clean prefix should pass through")
	}
	if strings.Contains(out, "forbidden") || strings.Contains(out, " more") {
		t.Errorf("violating/after content should be withheld: %s", out)
	}
	if !strings.Contains(out, "content_filter") {
		t.Error("should emit content_filter finish")
	}
}

func TestStream_AllowsClean(t *testing.T) {
	rec := httptest.NewRecorder()
	w := New(true, []string{"forbidden"}).Wrap(rec, true)
	w.WriteHeader(200)
	w.Write([]byte(`data: {"choices":[{"delta":{"content":"all good"}}]}` + "\n"))
	if !strings.Contains(rec.Body.String(), "all good") {
		t.Error("clean stream should pass")
	}
}

func TestNonStream_Blocks(t *testing.T) {
	rec := httptest.NewRecorder()
	w := New(true, []string{"forbidden"}).Wrap(rec, false)
	w.WriteHeader(200)
	w.Write([]byte(`{"choices":[{"message":{"content":"this is forbidden"}}]}`))
	w.Finalize()
	if !strings.Contains(rec.Body.String(), "content_filter") {
		t.Errorf("non-stream banned content should be blocked: %s", rec.Body.String())
	}
}

func TestNonStream_Allows(t *testing.T) {
	rec := httptest.NewRecorder()
	w := New(true, []string{"forbidden"}).Wrap(rec, false)
	w.WriteHeader(200)
	w.Write([]byte(`{"choices":[{"message":{"content":"clean answer"}}]}`))
	w.Finalize()
	if !strings.Contains(rec.Body.String(), "clean answer") {
		t.Error("clean non-stream should pass through")
	}
}

func TestDisabled(t *testing.T) {
	if New(false, []string{"x"}).Enabled() {
		t.Error("enabled=false should be disabled")
	}
	if New(true, nil).Enabled() {
		t.Error("empty word list should be disabled")
	}
}
