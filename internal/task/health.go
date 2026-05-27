// Package task 提供后台任务，如渠道主动健康探测。
package task

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/dispatch"
)

const probeTimeout = 5 * time.Second

// HealthChecker 周期性探测各渠道连通性，并将结果反馈给调度器的熔断器。
type HealthChecker struct {
	d        *dispatch.Dispatcher
	interval time.Duration
	client   *http.Client
	log      *zap.Logger
}

// NewHealthChecker 构建健康探测器。
func NewHealthChecker(d *dispatch.Dispatcher, interval time.Duration, log *zap.Logger) *HealthChecker {
	if interval <= 0 {
		interval = time.Minute
	}
	return &HealthChecker{
		d:        d,
		interval: interval,
		client:   &http.Client{Timeout: probeTimeout},
		log:      log,
	}
}

// Run 阻塞运行探测循环，直至 ctx 取消。
func (h *HealthChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	h.checkAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAll(ctx)
		}
	}
}

func (h *HealthChecker) checkAll(ctx context.Context) {
	for _, info := range h.d.List() {
		ok := h.probe(ctx, info.BaseURL)
		h.d.RecordResult(info.Name, ok)
		if !ok {
			h.log.Warn("channel health probe failed", zap.String("channel", info.Name))
		}
	}
}

// probe 探测连通性：能拿到任意 HTTP 响应即视为可达（不关心状态码）。
func (h *HealthChecker) probe(ctx context.Context, baseURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return false
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
