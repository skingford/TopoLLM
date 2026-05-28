// Package cache 提供网关响应缓存：精确匹配（含规范化）的基础语义缓存。
// 命中即短路返回不调用上游；非流式响应在成功后写入。Redis 或内存双后端。
// 真正的语义相似度（embedding + 向量库）作为未来扩展点。
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kingford/TopoLLM/internal/config"
)

// Backend 是缓存后端抽象。
type Backend interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration)
}

// Service 是响应缓存的对外门面。
type Service struct {
	enabled bool
	backend Backend
	ttl     time.Duration
}

// New 构建缓存服务；rdb 非空且启用时优先 Redis，否则进程内内存缓存。
func New(cfg config.SemanticCacheConfig, rdb *redis.Client) *Service {
	if !cfg.Enabled || cfg.TTLSeconds <= 0 {
		return &Service{enabled: false}
	}
	var b Backend
	if rdb != nil {
		b = &redisBackend{rdb: rdb}
	} else {
		b = newMemBackend()
	}
	return &Service{enabled: true, backend: b, ttl: time.Duration(cfg.TTLSeconds) * time.Second}
}

// Enabled 返回缓存是否启用。
func (s *Service) Enabled() bool { return s.enabled }

// KeyForChat 从 chat 请求体计算缓存键；流式或解析失败返回 ("", false)。
// 规范化：仅保留 model + messages(role,content)；role 小写、content 大小写不敏感 + 空白折叠。
func (s *Service) KeyForChat(body []byte) (string, bool) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false
	}
	if req.Stream {
		return "", false
	}
	type m struct {
		R string `json:"r"`
		C string `json:"c"`
	}
	norm := struct {
		Model string `json:"m"`
		Msgs  []m    `json:"x"`
	}{Model: req.Model}
	for _, msg := range req.Messages {
		norm.Msgs = append(norm.Msgs, m{
			R: strings.ToLower(strings.TrimSpace(msg.Role)),
			C: normalizeContent(msg.Content),
		})
	}
	canonical, _ := json.Marshal(norm)
	sum := sha256.Sum256(canonical)
	return "topollm:cache:chat:" + hex.EncodeToString(sum[:]), true
}

func normalizeContent(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// Get 取缓存。
func (s *Service) Get(ctx context.Context, key string) ([]byte, bool) {
	if !s.enabled {
		return nil, false
	}
	return s.backend.Get(ctx, key)
}

// Set 写缓存。
func (s *Service) Set(ctx context.Context, key string, value []byte) {
	if !s.enabled {
		return
	}
	s.backend.Set(ctx, key, value, s.ttl)
}

// ---- 后端：内存 ----

type memEntry struct {
	value   []byte
	expires time.Time
}

type memBackend struct {
	mu sync.Mutex
	m  map[string]memEntry
}

func newMemBackend() *memBackend {
	return &memBackend{m: map[string]memEntry{}}
}

func (b *memBackend) Get(_ context.Context, key string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.m[key]
	if !ok || time.Now().After(e.expires) {
		delete(b.m, key)
		return nil, false
	}
	return e.value, true
}

func (b *memBackend) Set(_ context.Context, key string, value []byte, ttl time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	b.m[key] = memEntry{value: cp, expires: time.Now().Add(ttl)}
}

// ---- 后端：Redis ----

type redisBackend struct {
	rdb *redis.Client
}

func (b *redisBackend) Get(ctx context.Context, key string) ([]byte, bool) {
	v, err := b.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return v, true
}

func (b *redisBackend) Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	_ = b.rdb.Set(ctx, key, value, ttl).Err()
}
