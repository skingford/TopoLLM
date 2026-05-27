package adaptor

import (
	"fmt"
	"sort"
	"sync"
)

// Factory 创建一个 Adaptor 实例。
type Factory func() Adaptor

var (
	mu       sync.RWMutex
	registry = make(map[string]Factory)
)

// Register 注册一个适配器工厂；重复名称会 panic（尽早暴露编码错误）。
// 通常在厂商包的 init() 中调用。
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("adaptor already registered: %s", name))
	}
	registry[name] = f
}

// Get 按名称返回一个新的 Adaptor 实例。
func Get(name string) (Adaptor, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, false
	}
	return f(), true
}

// Names 返回已注册的适配器名称（已排序），便于调试与展示。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
