package billing

import (
	"errors"
	"sync"
)

// ErrInsufficientQuota 表示令牌配额不足。
var ErrInsufficientQuota = errors.New("insufficient quota")

// Quota 维护每令牌的配额余额（单机内存；分布式 Redis/DB 版为后续阶段）。
// 未在初始配额中出现的令牌视为"不限额"。
type Quota struct {
	mu       sync.Mutex
	enabled  bool
	balances map[string]float64
	tracked  map[string]bool
}

// NewQuota 构建配额管理器。initial 为令牌->总额度。
func NewQuota(enabled bool, initial map[string]float64) *Quota {
	balances := make(map[string]float64, len(initial))
	tracked := make(map[string]bool, len(initial))
	for k, v := range initial {
		balances[k] = v
		tracked[k] = true
	}
	return &Quota{enabled: enabled, balances: balances, tracked: tracked}
}

// Reserve 预扣 amount；配额不足返回 ErrInsufficientQuota。未跟踪/未启用则放行。
func (q *Quota) Reserve(token string, amount float64) error {
	if !q.enabled || amount <= 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.tracked[token] {
		return nil
	}
	if q.balances[token] < amount {
		return ErrInsufficientQuota
	}
	q.balances[token] -= amount
	return nil
}

// Settle 结算：退还预扣与实际的差额（实际 > 预扣则继续扣减）。
func (q *Quota) Settle(token string, reserved, actual float64) {
	if !q.enabled {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.tracked[token] {
		return
	}
	q.balances[token] += reserved - actual
}

// Refund 全额退还预扣（请求失败时）。
func (q *Quota) Refund(token string, reserved float64) {
	q.Settle(token, reserved, 0)
}

// Balance 返回当前余额。
func (q *Quota) Balance(token string) float64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.balances[token]
}
