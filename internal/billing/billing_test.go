package billing

import (
	"testing"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/relay"
)

func TestPriceTable_Cost(t *testing.T) {
	pt := NewPriceTable(map[string]config.Price{"m": {Input: 1.0, Output: 2.0}})
	// 1e6*1/1e6 + 1e6*2/1e6 = 3
	if got := pt.Cost("m", 1_000_000, 1_000_000); got != 3.0 {
		t.Errorf("cost = %v, want 3", got)
	}
}

func TestPriceTable_Default(t *testing.T) {
	pt := NewPriceTable(map[string]config.Price{"default": {Input: 1, Output: 1}})
	if got := pt.Cost("unknown", 1_000_000, 0); got != 1.0 {
		t.Errorf("default cost = %v, want 1", got)
	}
}

func TestQuota_ReserveSettle(t *testing.T) {
	q := NewQuota(true, map[string]float64{"t": 10})
	if err := q.Reserve("t", 6); err != nil {
		t.Fatal(err)
	}
	if q.Balance("t") != 4 {
		t.Errorf("balance after reserve = %v, want 4", q.Balance("t"))
	}
	q.Settle("t", 6, 2) // 实际 2，退还 4 -> 8
	if q.Balance("t") != 8 {
		t.Errorf("balance after settle = %v, want 8", q.Balance("t"))
	}
}

func TestQuota_Insufficient(t *testing.T) {
	q := NewQuota(true, map[string]float64{"t": 1})
	if err := q.Reserve("t", 5); err != ErrInsufficientQuota {
		t.Errorf("err = %v, want ErrInsufficientQuota", err)
	}
}

func TestQuota_DisabledAndUntracked(t *testing.T) {
	if err := NewQuota(false, nil).Reserve("any", 999); err != nil {
		t.Errorf("disabled quota should allow: %v", err)
	}
	if err := NewQuota(true, map[string]float64{"known": 1}).Reserve("unknown", 999); err != nil {
		t.Errorf("untracked token should be unlimited: %v", err)
	}
}

func TestService_ReserveSettle(t *testing.T) {
	cfg := config.BillingConfig{
		Enabled: true,
		Pricing: map[string]config.Price{"m": {Input: 1, Output: 1}},
		Quotas:  map[string]float64{"tok": 100},
	}
	s := New(cfg, nil, zap.NewNop())

	reserved, err := s.Reserve("tok", "m", 1_000_000, 1_000_000) // est = 1 + 1 = 2
	if err != nil {
		t.Fatal(err)
	}
	if reserved != 2 {
		t.Errorf("reserved = %v, want 2", reserved)
	}
	// 实际仅用 prompt 1e6 -> actual = 1；balance = 100 - 2 + (2-1) = 99
	s.Settle("tok", "m", "req1", "ch", reserved, &relay.Usage{PromptTokens: 1_000_000})
	if got := s.quota.Balance("tok"); got != 99 {
		t.Errorf("balance = %v, want 99", got)
	}
}

func TestService_Refund(t *testing.T) {
	cfg := config.BillingConfig{Enabled: true, Pricing: map[string]config.Price{"m": {Input: 1, Output: 1}}, Quotas: map[string]float64{"tok": 10}}
	s := New(cfg, nil, zap.NewNop())
	reserved, _ := s.Reserve("tok", "m", 1_000_000, 0) // est = 1
	s.Refund("tok", reserved)
	if got := s.quota.Balance("tok"); got != 10 {
		t.Errorf("balance after refund = %v, want 10", got)
	}
}

func TestMaskToken(t *testing.T) {
	if got := maskToken("sk-abcd1234"); got != "****1234" {
		t.Errorf("mask = %q", got)
	}
	if got := maskToken("ab"); got != "****" {
		t.Errorf("mask short = %q", got)
	}
}
