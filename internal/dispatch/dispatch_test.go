package dispatch

import (
	"testing"
	"time"

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
	excluded := map[string]bool{"c1": true, "c2": true, "c3": true}
	if _, err := d.Select("gpt", excluded); err == nil {
		t.Error("disabled channel should not be selectable")
	}
}

func TestSelect_SkipsOpenBreaker(t *testing.T) {
	d := New(testChannels(), WithBreaker(1, time.Hour))
	d.RecordResult("c1", false) // 阈值=1，立即熔断
	d.RecordResult("c2", false)
	ch, err := d.Select("gpt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Name != "c3" {
		t.Errorf("expected c3 after c1/c2 breakers open, got %s", ch.Name)
	}
}

func TestBreaker_RecoversOnSuccess(t *testing.T) {
	d := New(testChannels(), WithBreaker(1, time.Hour))
	d.RecordResult("c1", false) // 打开
	d.RecordResult("c1", true)  // 成功关闭
	ch, err := d.Select("gpt", map[string]bool{"c2": true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Name != "c1" {
		t.Errorf("c1 should recover after success, got %s", ch.Name)
	}
}

func TestAddRemoveList(t *testing.T) {
	d := New(nil)
	d.Add(config.ChannelConfig{Name: "x", Adaptor: "openai", BaseURL: "http://h/v1", Models: []string{"m"}, Enabled: true})
	if len(d.List()) != 1 {
		t.Fatalf("list len = %d, want 1", len(d.List()))
	}
	ch, err := d.Select("m", nil)
	if err != nil || ch.Name != "x" {
		t.Errorf("select after add: ch=%v err=%v", ch, err)
	}

	if !d.Remove("x") {
		t.Error("Remove should return true for existing channel")
	}
	if _, err := d.Select("m", nil); err == nil {
		t.Error("select after remove should fail")
	}
	if len(d.List()) != 0 {
		t.Errorf("list len after remove = %d, want 0", len(d.List()))
	}
}

func TestAdd_ReplacesSameName(t *testing.T) {
	d := New(nil)
	d.Add(config.ChannelConfig{Name: "x", Adaptor: "openai", BaseURL: "http://a/v1", Models: []string{"m"}, Enabled: true})
	d.Add(config.ChannelConfig{Name: "x", Adaptor: "openai", BaseURL: "http://b/v1", Models: []string{"m"}, Enabled: true})
	if len(d.List()) != 1 {
		t.Errorf("same-name add should replace, list = %d", len(d.List()))
	}
}
