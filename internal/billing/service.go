package billing

import (
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kingford/TopoLLM/internal/config"
	"github.com/kingford/TopoLLM/internal/model"
	"github.com/kingford/TopoLLM/internal/observability"
	"github.com/kingford/TopoLLM/internal/relay"
)

// quotaStore 抽象配额存储，便于内存与 DB 两种实现互换。
type quotaStore interface {
	Reserve(token string, amount float64) error
	Settle(token string, reserved, actual float64)
	Balance(token string) float64
}

// Service 统一计费：定价、配额（三阶段消费）、用量记录。
type Service struct {
	prices *PriceTable
	quota  quotaStore
	db     *gorm.DB
	log    *zap.Logger
}

// New 构建计费服务。db 非空且 billing 启用时配额持久化到 DB，否则用单机内存配额。
func New(cfg config.BillingConfig, db *gorm.DB, log *zap.Logger) *Service {
	var q quotaStore
	if cfg.Enabled && db != nil {
		q = newDBQuota(db, cfg.Quotas, log)
	} else {
		q = NewQuota(cfg.Enabled, cfg.Quotas)
	}
	return &Service{
		prices: NewPriceTable(cfg.Pricing),
		quota:  q,
		db:     db,
		log:    log,
	}
}

// Reserve 预扣估算费用；返回预扣额，配额不足返回 ErrInsufficientQuota。
func (s *Service) Reserve(token, modelName string, promptTokens, maxTokens int) (float64, error) {
	est := s.prices.Estimate(modelName, promptTokens, maxTokens)
	if err := s.quota.Reserve(token, est); err != nil {
		return 0, err
	}
	return est, nil
}

// Refund 请求失败时全额退还预扣。
func (s *Service) Refund(token string, reserved float64) {
	s.quota.Settle(token, reserved, 0)
}

// Settle 结算实际费用：退还差额、累加费用指标、记录用量。
func (s *Service) Settle(token, modelName, requestID, channel string, reserved float64, usage *relay.Usage) {
	var cost float64
	if usage != nil {
		cost = s.prices.Cost(modelName, usage.PromptTokens, usage.CompletionTokens)
	}
	s.quota.Settle(token, reserved, cost)
	observability.BillingCost.WithLabelValues(channel, modelName).Add(cost)
	s.recordUsage(token, modelName, requestID, channel, cost, usage)
}

func (s *Service) recordUsage(token, modelName, requestID, channel string, cost float64, usage *relay.Usage) {
	if s.db == nil {
		return
	}
	rec := &model.UsageLog{
		RequestID: requestID,
		TokenKey:  maskToken(token),
		Channel:   channel,
		Model:     modelName,
		Cost:      cost,
		CreatedAt: time.Now(),
	}
	if usage != nil {
		rec.PromptTokens = usage.PromptTokens
		rec.CompletionTokens = usage.CompletionTokens
		rec.TotalTokens = usage.TotalTokens
	}
	if err := s.db.Create(rec).Error; err != nil {
		s.log.Warn("failed to write usage log", zap.Error(err))
	}
}

// maskToken 仅保留尾部 4 位，避免在用量日志中持久化完整令牌。
func maskToken(t string) string {
	if len(t) <= 4 {
		return "****"
	}
	return "****" + t[len(t)-4:]
}
