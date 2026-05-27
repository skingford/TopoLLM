package dispatch

import (
	"testing"

	"github.com/kingford/TopoLLM/internal/config"
)

func testChannels() []config.ChannelConfig {
	return []config.ChannelConfig{
		{Name: "c1", Adaptor: "openai", Models: []string{"gpt"}, Weight: 1, Priority: 10, Enabled: true},
		{Name: "c2", Adaptor: "openai", Models: []string{"gpt"}, Weight: 1, Priority: 10, Enabled: true},
		{Name: "c3", Adaptor: "openai", Models: []string{"gpt"}, Weight: 1, Priority: 1, Enabled: true},
		{Name: "disabled", Adaptor: "openai", Models: []string{"gpt"}, Enabled: false},
	}
}

func TestSelect_UnknownModel(t *testing.T) {
	d := New(testChannels())
	if _, err := d.Select("nope", nil); err == nil {
		t.Error("expected error for unknown model")
	}
}

func TestSelect_PrefersHighestPriority(t *testing.T) {
	d := New(testChannels())
	for i := 0; i < 20; i++ {
		ch, err := d.Select("gpt", nil)
		if err != nil {
			t.Fatal(err)
		}
		if ch.Name == "c3" {
			t.Errorf("selected low-priority c3 over priority-10 group")
		}
	}
}

func TestSelect_Failover(t *testing.T) {
	d := New(testChannels())
	excluded := map[string]bool{"c1": true, "c2": true}
	ch, err := d.Select("gpt", excluded)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Name != "c3" {
		t.Errorf("expected failover to c3, got %s", ch.Name)
	}
}

func TestSelect_AllExcluded(t *testing.T) {
	d := New(testChannels())
	excluded := map[string]bool{"c1": true, "c2": true, "c3": true}
	if _, err := d.Select("gpt", excluded); err == nil {
		t.Error("expected error when all channels excluded")
	}
}

func TestSelect_DisabledNotSelectable(t *testing.T) {
	d := New(testChannels())
	// 已禁用渠道不应进入候选：排除其余 3 个后应无可用渠道。
	excluded := map[string]bool{"c1": true, "c2": true, "c3": true}
	if _, err := d.Select("gpt", excluded); err == nil {
		t.Error("disabled channel should not be selectable")
	}
}
