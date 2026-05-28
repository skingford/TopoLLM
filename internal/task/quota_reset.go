package task

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/billing"
)

// QuotaResetter 周期性把配置中各令牌的配额余额重置为初始值（用于周期性配额刷新）。
type QuotaResetter struct {
	bill     *billing.Service
	initial  map[string]float64
	interval time.Duration
	log      *zap.Logger
}

// NewQuotaResetter 构建配额重置任务。interval <= 0 时回退为 24 小时。
func NewQuotaResetter(bill *billing.Service, initial map[string]float64, interval time.Duration, log *zap.Logger) *QuotaResetter {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &QuotaResetter{bill: bill, initial: initial, interval: interval, log: log}
}

// Run 阻塞运行重置循环，直至 ctx 取消。
func (r *QuotaResetter) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.bill.ResetAll(r.initial)
			r.log.Info("quota reset complete", zap.Int("tokens", len(r.initial)))
		}
	}
}
