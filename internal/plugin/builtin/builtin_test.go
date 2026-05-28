package builtin

import (
	"net/http"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/plugin"
)

func TestSensitiveWords_Blocks(t *testing.T) {
	p, _ := newSensitiveWords(map[string]any{"words": []any{"forbidden"}})
	res := p.OnRequest(&plugin.Context{Body: []byte(`{"messages":[{"content":"this is forbidden"}]}`)})
	if res.Action != plugin.ActionReject || res.Status != http.StatusBadRequest {
		t.Errorf("res = %+v, want reject 400", res)
	}
}

func TestSensitiveWords_Allows(t *testing.T) {
	p, _ := newSensitiveWords(map[string]any{"words": []any{"forbidden"}})
	if res := p.OnRequest(&plugin.Context{Body: []byte("clean content")}); res.Action != plugin.ActionContinue {
		t.Error("clean content should pass")
	}
}

func TestAudit_Continues(t *testing.T) {
	p, _ := newAudit(nil)
	if res := p.OnRequest(&plugin.Context{Log: zap.NewNop()}); res.Action != plugin.ActionContinue {
		t.Error("audit should continue")
	}
}

func TestPIIMask_Email(t *testing.T) {
	p, _ := newPIIMask(nil)
	ctx := &plugin.Context{Body: []byte(`{"messages":[{"role":"user","content":"My email is alice@example.com please reply"}]}`)}
	if res := p.OnRequest(ctx); res.Action != plugin.ActionContinue {
		t.Fatal("pii_mask should continue")
	}
	body := string(ctx.Body)
	if strings.Contains(body, "alice@example.com") {
		t.Errorf("email should be redacted: %s", body)
	}
	if !strings.Contains(body, "[EMAIL]") {
		t.Errorf("expected [EMAIL] placeholder: %s", body)
	}
}

func TestPIIMask_Phone(t *testing.T) {
	p, _ := newPIIMask(nil)
	ctx := &plugin.Context{Body: []byte(`{"messages":[{"role":"user","content":"Call me at 415-555-0100"}]}`)}
	p.OnRequest(ctx)
	if !strings.Contains(string(ctx.Body), "[PHONE]") {
		t.Errorf("expected [PHONE]: %s", ctx.Body)
	}
}

func TestPIIMask_SSN(t *testing.T) {
	p, _ := newPIIMask(nil)
	ctx := &plugin.Context{Body: []byte(`{"messages":[{"role":"user","content":"SSN 123-45-6789 for verification"}]}`)}
	p.OnRequest(ctx)
	if !strings.Contains(string(ctx.Body), "[SSN]") {
		t.Errorf("expected [SSN]: %s", ctx.Body)
	}
}

func TestPIIMask_Custom(t *testing.T) {
	p, _ := newPIIMask(map[string]any{
		"custom": []any{
			map[string]any{"pattern": `SECRET-\d+`, "replace": "[CUSTOM]"},
		},
	})
	ctx := &plugin.Context{Body: []byte(`{"messages":[{"role":"user","content":"My key SECRET-12345 here"}]}`)}
	p.OnRequest(ctx)
	if !strings.Contains(string(ctx.Body), "[CUSTOM]") {
		t.Errorf("custom pattern not applied: %s", ctx.Body)
	}
}

func TestPIIMask_NonJSON_PassesThrough(t *testing.T) {
	p, _ := newPIIMask(nil)
	original := []byte("not json with alice@example.com")
	ctx := &plugin.Context{Body: append([]byte(nil), original...)}
	p.OnRequest(ctx)
	if string(ctx.Body) != string(original) {
		t.Error("non-JSON body should not be modified")
	}
}
