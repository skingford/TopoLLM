package plugin

import (
	"net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
)

type allowPlugin struct{ name string }

func (p allowPlugin) Name() string            { return p.name }
func (allowPlugin) OnRequest(*Context) Result { return Continue() }

type denyPlugin struct{}

func (denyPlugin) Name() string              { return "deny" }
func (denyPlugin) OnRequest(*Context) Result { return Reject(http.StatusForbidden, "nope") }

func init() {
	Register("test-allow", func(map[string]any) (Plugin, error) { return allowPlugin{name: "test-allow"}, nil })
	Register("test-deny", func(map[string]any) (Plugin, error) { return denyPlugin{}, nil })
}

func TestChain_Continue(t *testing.T) {
	chain, err := BuildChain([]config.PluginConfig{{Name: "test-allow", Enabled: true}}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if res := chain.OnRequest(&Context{}); res.Action != ActionContinue {
		t.Errorf("action = %v, want continue", res.Action)
	}
}

func TestChain_Reject(t *testing.T) {
	chain, _ := BuildChain([]config.PluginConfig{{Name: "test-deny", Enabled: true}}, zap.NewNop())
	res := chain.OnRequest(&Context{})
	if res.Action != ActionReject || res.Status != http.StatusForbidden {
		t.Errorf("res = %+v, want reject 403", res)
	}
}

func TestChain_UnknownPlugin(t *testing.T) {
	if _, err := BuildChain([]config.PluginConfig{{Name: "nope", Enabled: true}}, zap.NewNop()); err == nil {
		t.Error("expected error for unknown plugin")
	}
}

func TestChain_DisabledSkipped(t *testing.T) {
	chain, err := BuildChain([]config.PluginConfig{{Name: "test-deny", Enabled: false}}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if res := chain.OnRequest(&Context{}); res.Action != ActionContinue {
		t.Error("disabled plugin should be skipped")
	}
}

func TestChain_PriorityOrder(t *testing.T) {
	chain, _ := BuildChain([]config.PluginConfig{
		{Name: "test-allow", Enabled: true, Priority: 1},
		{Name: "test-deny", Enabled: true, Priority: 10},
	}, zap.NewNop())
	if res := chain.OnRequest(&Context{}); res.Action != ActionReject {
		t.Error("high-priority deny should reject first")
	}
}
