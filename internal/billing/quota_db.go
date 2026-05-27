package billing

import (
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/kingford/TopoLLM/internal/model"
)

// dbQuota 是 DB 持久化的配额存储；用原子 SQL 更新保证跨实例一致、重启不丢。
type dbQuota struct {
	db      *gorm.DB
	tracked map[string]bool
	log     *zap.Logger
}

func newDBQuota(db *gorm.DB, initial map[string]float64, log *zap.Logger) *dbQuota {
	tracked := make(map[string]bool, len(initial))
	for token, amount := range initial {
		tracked[token] = true
		// 仅在不存在时按配置初始化余额（不覆盖已有余额）。
		row := model.TokenQuota{TokenKey: token}
		if err := db.Where(model.TokenQuota{TokenKey: token}).
			Attrs(model.TokenQuota{Balance: amount}).
			FirstOrCreate(&row).Error; err != nil {
			log.Warn("seed quota failed", zap.String("token", maskToken(token)), zap.Error(err))
		}
	}
	return &dbQuota{db: db, tracked: tracked, log: log}
}

// Reserve 原子预扣：仅当 balance >= amount 时扣减；否则配额不足。
func (q *dbQuota) Reserve(token string, amount float64) error {
	if amount <= 0 || !q.tracked[token] {
		return nil
	}
	res := q.db.Model(&model.TokenQuota{}).
		Where("token_key = ? AND balance >= ?", token, amount).
		Update("balance", gorm.Expr("balance - ?", amount))
	if res.Error != nil {
		q.log.Warn("quota reserve db error (fail-open)", zap.Error(res.Error))
		return nil // fail-open：配额组件 DB 故障时不阻断业务
	}
	if res.RowsAffected == 0 {
		return ErrInsufficientQuota
	}
	return nil
}

// Settle 退还预扣与实际的差额。
func (q *dbQuota) Settle(token string, reserved, actual float64) {
	if !q.tracked[token] {
		return
	}
	if err := q.db.Model(&model.TokenQuota{}).
		Where("token_key = ?", token).
		Update("balance", gorm.Expr("balance + ?", reserved-actual)).Error; err != nil {
		q.log.Warn("quota settle db error", zap.Error(err))
	}
}

// Balance 返回当前余额（查询失败返回 0）。
func (q *dbQuota) Balance(token string) float64 {
	var row model.TokenQuota
	if err := q.db.Where("token_key = ?", token).First(&row).Error; err != nil {
		return 0
	}
	return row.Balance
}
