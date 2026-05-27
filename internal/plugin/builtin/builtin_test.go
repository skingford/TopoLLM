package builtin

import (
	"net/http"
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
