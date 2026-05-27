// Package plugin 提供请求前置插件管线：注册表 + 按优先级排序的 Hook 链 + 短路拒绝。
// 新增插件只需实现 Plugin 接口并通过 Register 注册，无需改动核心代码。
package plugin

import (
	"fmt"
	"sort"

	"go.uber.org/zap"

	"github.com/kingford/TopoLLM/internal/config"
)

// Action 是插件对请求的处置。
type Action int

const (
	// ActionContinue 放行，继续后续处理。
	ActionContinue Action = iota
	// ActionReject 短路拒绝。
	ActionReject
)

// Result 是插件的处置结果。
type Result struct {
	Action  Action
	Status  int    // ActionReject 时的 HTTP 状态码
	Message string // ActionReject 时的错误消息
}

// Continue 返回放行结果。
func Continue() Result { return Result{Action: ActionContinue} }

// Reject 返回短路拒绝结果。
func Reject(status int, msg string) Result {
	return Result{Action: ActionReject, Status: status, Message: msg}
}

// Context 是请求在插件链中的可变上下文。插件可读取字段并改写 Body。
type Context struct {
	RequestID string
	Token     string
	Model     string
	Stream    bool
	Body      []byte
	Log       *zap.Logger
}

// Plugin 是请求前置拦截器：检查/改写请求，或短路拒绝。
type Plugin interface {
	Name() string
	OnRequest(*Context) Result
}

// Factory 由插件名与选项构造插件实例。
type Factory func(options map[string]any) (Plugin, error)

var registry = map[string]Factory{}

// Register 注册一个插件工厂（通常在插件包 init() 中调用）。
func Register(name string, f Factory) {
	if _, ok := registry[name]; ok {
		panic("plugin already registered: " + name)
	}
	registry[name] = f
}

type entry struct {
	plugin   Plugin
	priority int
}

// Chain 是按优先级排序的插件链。
type Chain struct {
	entries []entry
	log     *zap.Logger
}

// BuildChain 按配置构建插件链：仅启用项；按 priority 降序（高优先级先执行）。
func BuildChain(cfgs []config.PluginConfig, log *zap.Logger) (*Chain, error) {
	var entries []entry
	for _, pc := range cfgs {
		if !pc.Enabled {
			continue
		}
		factory, ok := registry[pc.Name]
		if !ok {
			return nil, fmt.Errorf("unknown plugin: %s", pc.Name)
		}
		p, err := factory(pc.Options)
		if err != nil {
			return nil, fmt.Errorf("init plugin %s: %w", pc.Name, err)
		}
		entries = append(entries, entry{plugin: p, priority: pc.Priority})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].priority > entries[j].priority })
	return &Chain{entries: entries, log: log}, nil
}

// OnRequest 依次执行插件；首个 Reject 立即短路返回。
func (c *Chain) OnRequest(ctx *Context) Result {
	for _, e := range c.entries {
		if res := e.plugin.OnRequest(ctx); res.Action == ActionReject {
			c.log.Info("plugin rejected request",
				zap.String("plugin", e.plugin.Name()),
				zap.String("request_id", ctx.RequestID),
			)
			return res
		}
	}
	return Continue()
}
