// Package model 定义网关的持久化数据模型（GORM）。
package model

import "time"

// Channel 是上游供应商实例的持久化模型（Phase 2 完善并接入 GORM 迁移）。
type Channel struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	Adaptor   string    `gorm:"size:64;not null" json:"adaptor"`
	BaseURL   string    `gorm:"size:512" json:"base_url"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	Weight    int       `gorm:"default:1" json:"weight"`
	Priority  int       `gorm:"default:0" json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Token 是对外发放的 API 令牌（Phase 5 完善鉴权与配额）。
type Token struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Name      string    `gorm:"size:128" json:"name"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UsageLog 记录每次成功转发的用量与费用（写入独立日志库以隔离高频写）。
type UsageLog struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	RequestID        string    `gorm:"size:64;index" json:"request_id"`
	TokenKey         string    `gorm:"size:32;index" json:"token_key"` // 脱敏（尾 4 位）
	Channel          string    `gorm:"size:128" json:"channel"`
	Model            string    `gorm:"size:128;index" json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	Cost             float64   `json:"cost"`
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}
