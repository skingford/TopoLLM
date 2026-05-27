// Package dispatch 负责按模型选择上游渠道：优先级分组 + 组内加权随机 + 故障转移支持。
package dispatch

import (
	"errors"
	"math/rand"
	"sync"

	"github.com/kingford/TopoLLM/internal/adaptor"
	"github.com/kingford/TopoLLM/internal/config"
)

// ErrNoChannel 表示该模型没有可用渠道。
var ErrNoChannel = errors.New("no available channel for model")

// Dispatcher 维护模型到渠道的映射并执行渠道选择。
type Dispatcher struct {
	mu      sync.RWMutex
	byModel map[string][]*adaptor.Channel
}

// New 从配置构建调度器（仅纳入已启用渠道）。
func New(channels []config.ChannelConfig) *Dispatcher {
	byModel := make(map[string][]*adaptor.Channel)
	for _, cc := range channels {
		if !cc.Enabled {
			continue
		}
		ch := &adaptor.Channel{
			Name:     cc.Name,
			Adaptor:  cc.Adaptor,
			BaseURL:  cc.BaseURL,
			APIKey:   cc.APIKey,
			Weight:   max(cc.Weight, 1),
			Priority: cc.Priority,
		}
		for _, m := range cc.Models {
			byModel[m] = append(byModel[m], ch)
		}
	}
	return &Dispatcher{byModel: byModel}
}

// Select 返回某模型的一个可用渠道：取最高优先级分组，组内按权重随机。
// excluded 用于故障转移时跳过已尝试过的渠道（按渠道名）。
func (d *Dispatcher) Select(model string, excluded map[string]bool) (*adaptor.Channel, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	candidates := d.byModel[model]
	if len(candidates) == 0 {
		return nil, ErrNoChannel
	}

	highest := 0
	found := false
	for _, ch := range candidates {
		if excluded[ch.Name] {
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
		if excluded[ch.Name] || ch.Priority != highest {
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
