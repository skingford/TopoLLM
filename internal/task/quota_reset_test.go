package task

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/billing"
	"github.com/kingford/TopoLLM/internal/config"
)

func TestQuotaResetter_ResetsBalance(t *testing.T) {
	cfg := config.BillingConfig{
		Enabled: true,
		Pricing: map[string]config.Price{"m": {Input: 1, Output: 1}},
		Quotas:  map[string]float64{"tok": 50},
	}
	s := billing.New(cfg, nil, zap.NewNop())

	// 消耗到 49。
	if _, err := s.Reserve("tok", "m", 1_000_000, 0); err != nil {
		t.Fatal(err)
	}
	if got := s.Balance("tok"); got != 49 {
		t.Fatalf("setup: balance = %v, want 49", got)
	}

	// 手动触发一次重置（绕过 ticker，直接调用底层逻辑）。
	r := NewQuotaResetter(s, cfg.Quotas, time.Minute, zap.NewNop())
	r.bill.ResetAll(r.initial)

	if got := s.Balance("tok"); got != 50 {
		t.Errorf("after reset balance = %v, want 50", got)
	}
}
