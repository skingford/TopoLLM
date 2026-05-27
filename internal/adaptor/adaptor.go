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
// Extra 承载 adaptor 专属参数（如 azure 的 api_version / deployment）。
type Channel struct {
	Name     string
	Adaptor  string
	BaseURL  string
	APIKey   string
	Weight   int
	Priority int
	Extra    map[string]string
}

// Request 是经网关解析后的统一调用请求（对外 OpenAI 格式）。
type Request struct {
	Model  string
	Stream bool
	Body   []byte // 原始 OpenAI 格式请求体
}

// Adaptor 把统一的 OpenAI schema 翻译为某厂商协议，并把上游响应写回客户端。
type Adaptor interface {
	// Name 返回适配器的唯一标识。
	Name() string
	// Capabilities 返回该适配器支持的能力。
	Capabilities() Capabilities
	// SetupRequest 构造面向上游的 chat/completions 请求（含鉴权、URL、body 转换）。
	SetupRequest(ctx context.Context, in *Request, ch *Channel) (*http.Request, error)
	// RelayResponse 将上游响应写回 w（处理流式与非流式），并尽力返回用量。
	RelayResponse(w http.ResponseWriter, resp *http.Response, stream bool) (*relay.Usage, error)
}

// PathAdaptor 是可选能力接口：支持任意 OpenAI 兼容子路径（embeddings、images/generations、rerank 等）。
// 调度层通过类型断言探测，未实现者视为不支持该类端点。
type PathAdaptor interface {
	SetupPath(ctx context.Context, in *Request, ch *Channel, path string) (*http.Request, error)
}
