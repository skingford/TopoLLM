// Package dispatch 负责按模型选择上游渠道：优先级分组 + 组内加权随机 + 故障转移 + 熔断。
// 支持渠道运行时 CRUD（供管理 API 使用）。
package dispatch

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/config"
)

// ErrNoChannel 表示该模型没有可用渠道。
var ErrNoChannel = errors.New("no available channel for model")

const (
	defaultBreakerThreshold = 5
	defaultBreakerCooldown  = 30 * time.Second
)

// ChannelInfo 是渠道的只读快照（用于管理 API 展示）。
type ChannelInfo struct {
	Name     string   `json:"name"`
	Adaptor  string   `json:"adaptor"`
	BaseURL  string   `json:"base_url"`
	Models   []string `json:"models"`
	Weight   int      `json:"weight"`
	Priority int      `json:"priority"`
}

type channelMeta struct {
	ch     *adaptor.Channel
	models []string
}

// Dispatcher 维护模型到渠道的映射并执行渠道选择与熔断。
type Dispatcher struct {
	mu       sync.RWMutex
	byModel  map[string][]*adaptor.Channel
	channels map[string]*channelMeta
	breaker  *breaker
}

// Option 配置 Dispatcher。
type Option func(*Dispatcher)

// WithBreaker 配置熔断阈值（连续失败数）与冷却时长。
func WithBreaker(threshold int, cooldown time.Duration) Option {
	return func(d *Dispatcher) {
		if threshold <= 0 {
			threshold = defaultBreakerThreshold
		}
		if cooldown <= 0 {
			cooldown = defaultBreakerCooldown
		}
		d.breaker = newBreaker(threshold, cooldown)
	}
}

// New 从配置构建调度器（仅纳入已启用渠道）。
func New(channels []config.ChannelConfig, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		byModel:  make(map[string][]*adaptor.Channel),
		channels: make(map[string]*channelMeta),
		breaker:  newBreaker(defaultBreakerThreshold, defaultBreakerCooldown),
	}
	for _, cc := range channels {
		if !cc.Enabled {
			continue
		}
		d.addLocked(cc)
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// addLocked 在持锁（或构造期）下添加渠道。
func (d *Dispatcher) addLocked(cc config.ChannelConfig) {
	ch := &adaptor.Channel{
		Name:     cc.Name,
		Adaptor:  cc.Adaptor,
		BaseURL:  cc.BaseURL,
		APIKey:   cc.APIKey,
		Weight:   max(cc.Weight, 1),
		Priority: cc.Priority,
	}
	d.channels[cc.Name] = &channelMeta{ch: ch, models: cc.Models}
	for _, m := range cc.Models {
		d.byModel[m] = append(d.byModel[m], ch)
	}
}

// Add 运行时新增渠道（同名先移除再添加）。
func (d *Dispatcher) Add(cc config.ChannelConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.channels[cc.Name]; ok {
		d.removeLocked(cc.Name)
	}
	d.addLocked(cc)
}

// Remove 运行时移除渠道；返回是否存在。
func (d *Dispatcher) Remove(name string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.removeLocked(name)
}

func (d *Dispatcher) removeLocked(name string) bool {
	meta, ok := d.channels[name]
	if !ok {
		return false
	}
	delete(d.channels, name)
	for _, m := range meta.models {
		var filtered []*adaptor.Channel
		for _, ch := range d.byModel[m] {
			if ch.Name != name {
				filtered = append(filtered, ch)
			}
		}
		if len(filtered) == 0 {
			delete(d.byModel, m)
		} else {
			d.byModel[m] = filtered
		}
	}
	return true
}

// List 返回所有渠道的只读快照。
func (d *Dispatcher) List() []ChannelInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ChannelInfo, 0, len(d.channels))
	for _, meta := range d.channels {
		out = append(out, ChannelInfo{
			Name:     meta.ch.Name,
			Adaptor:  meta.ch.Adaptor,
			BaseURL:  meta.ch.BaseURL,
			Models:   meta.models,
			Weight:   meta.ch.Weight,
			Priority: meta.ch.Priority,
		})
	}
	return out
}

// RecordResult 上报某渠道一次调用的成败，驱动熔断状态。
func (d *Dispatcher) RecordResult(channel string, ok bool) {
	d.breaker.record(channel, ok)
}

// Select 返回某模型的一个可用渠道：取最高优先级分组，组内按权重随机。
// 跳过 excluded（故障转移）与熔断打开的渠道。
func (d *Dispatcher) Select(model string, excluded map[string]bool) (*adaptor.Channel, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	candidates := d.byModel[model]
	if len(candidates) == 0 {
		return nil, ErrNoChannel
	}

	available := func(ch *adaptor.Channel) bool {
		return !excluded[ch.Name] && d.breaker.allow(ch.Name)
	}

	highest := 0
	found := false
	for _, ch := range candidates {
		if !available(ch) {
			continue
		}
		if !found || ch.Priority > highest {
			highest = ch.Priority
			found = true
		}
	}
	if !found {
		return nil, ErrNoChannel
	}

	var group []*adaptor.Channel
	total := 0
	for _, ch := range candidates {
		if !available(ch) || ch.Priority != highest {
			continue
		}
		group = append(group, ch)
		total += ch.Weight
	}
	if len(group) == 1 || total <= 0 {
		return group[0], nil
	}

	r := rand.Intn(total)
	for _, ch := range group {
		r -= ch.Weight
		if r < 0 {
			return ch, nil
		}
	}
	return group[len(group)-1], nil
}

// ---- 被动熔断器 ----

type breakerEntry struct {
	failures  int
	openUntil time.Time
}

type breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	entries   map[string]*breakerEntry
}

func newBreaker(threshold int, cooldown time.Duration) *breaker {
	return &breaker{
		threshold: threshold,
		cooldown:  cooldown,
		entries:   make(map[string]*breakerEntry),
	}
}

// allow 判断渠道当前是否可用：熔断打开且未过冷却期则不可用；冷却结束后半开放行试探。
func (b *breaker) allow(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.entries[name]
	if !ok || e.openUntil.IsZero() {
		return true
	}
	return time.Now().After(e.openUntil)
}

// record 上报成败：成功重置；失败累计达阈值则打开熔断并进入冷却。
func (b *breaker) record(name string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[name]
	if e == nil {
		e = &breakerEntry{}
		b.entries[name] = e
	}
	if ok {
		e.failures = 0
		e.openUntil = time.Time{}
		return
	}
	e.failures++
	if e.failures >= b.threshold {
		e.openUntil = time.Now().Add(b.cooldown)
	}
}
