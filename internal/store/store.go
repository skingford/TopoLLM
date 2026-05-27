// Package store 初始化与持有数据层连接（GORM 数据库 + Redis）。
package store

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/kingford/TopoLLM/internal/config"
)

// Store 持有数据层连接。DB/Redis 未启用时对应字段为 nil。
type Store struct {
	DB    *gorm.DB
	Redis *redis.Client
}

// New 按配置初始化数据层；未启用的组件优雅跳过（便于本地起步）。
func New(ctx context.Context, cfg *config.Config) (*Store, error) {
	s := &Store{}

	if cfg.Database.Enabled {
		db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("connect database: %w", err)
		}
		s.DB = db
	}

	if cfg.Redis.Enabled {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("ping redis: %w", err)
		}
		s.Redis = rdb
	}

	return s, nil
}

// Close 释放数据层连接。
func (s *Store) Close() error {
	if s.Redis != nil {
		if err := s.Redis.Close(); err != nil {
			return err
		}
	}
	if s.DB != nil {
		sqlDB, err := s.DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}
