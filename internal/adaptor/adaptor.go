// Package adaptor 定义南向供应商适配器的 SPI：统一 OpenAI schema 与各厂商协议的互转。
// 新增厂商只需实现 Adaptor 接口并通过 Register 注册，无需改动核心代码。
package adaptor

import (
	"context"
	"net/http"

	"github.com/kingford/TopoLLM/internal/relay"
)

// Capabilities 声明一个适配器/渠道支持的能力，供调度层做能力协商。
type Capabilities struct {
	Chat       bool
	Stream     bool
	Tools      bool
	Vision     bool
	Embeddings bool
	Reasoning  bool
}

// Channel 是一个上游供应商实例（数据驱动，运行时可定义）。
// Phase 0 仅占位；Phase 2 接入数据库与完整字段（模型映射、quirks、权重等）。
type Channel struct {
	Name    string
	Adaptor string
	BaseURL string
	APIKey  string
}

// Adaptor 把统一的 OpenAI schema 翻译为某厂商协议，并解析其响应。
type Adaptor interface {
	// Name 返回适配器的唯一标识。
	Name() string
	// Capabilities 返回该适配器支持的能力。
	Capabilities() Capabilities
	// ConvertRequest 将统一请求转换为面向上游的 HTTP 请求。
	ConvertRequest(ctx context.Context, req *relay.ChatRequest, ch *Channel) (*http.Request, error)
	// ParseResponse 解析上游的非流式响应为统一结构与用量。
	ParseResponse(ctx context.Context, resp *http.Response) (*relay.ChatResponse, *relay.Usage, error)
	// 注：流式解析 (ParseStream) 在 Phase 1 引入流式时补充。
}
