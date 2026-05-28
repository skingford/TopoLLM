// Package tokenizer 提供基于 OpenAI tiktoken 的精确 token 计数。
// 用于计费预扣的估算上界（结算仍以上游返回的真实 usage 为准）。
// 非 OpenAI 模型回退到 cl100k_base 编码作为近似——足够用于"预扣不亏"的目标。
package tokenizer

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

// 使用离线 BPE loader，避免运行/测试时联网下载词表。
func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

var (
	mu    sync.Mutex
	cache = map[string]*tiktoken.Tiktoken{}
)

func encoder(name string) *tiktoken.Tiktoken {
	mu.Lock()
	defer mu.Unlock()
	if enc, ok := cache[name]; ok {
		return enc
	}
	enc, err := tiktoken.GetEncoding(name)
	if err != nil {
		return nil
	}
	cache[name] = enc
	return enc
}

// encodingFor 按模型名选择编码：GPT-4o/o*/gpt-4.1/gpt-5 -> o200k_base；其余 -> cl100k_base。
func encodingFor(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "gpt-4o"),
		strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"),
		strings.HasPrefix(m, "gpt-4.1"),
		strings.HasPrefix(m, "gpt-5"):
		return "o200k_base"
	default:
		return "cl100k_base"
	}
}

// CountText 估算 text 在指定模型下的 token 数；编码器不可用时回退到字节/4。
func CountText(text, model string) int {
	if text == "" {
		return 0
	}
	enc := encoder(encodingFor(model))
	if enc == nil {
		return len(text) / 4
	}
	return len(enc.Encode(text, nil, nil))
}

// CountChatMessages 从 OpenAI chat 请求体精确计算 prompt token 数。
// 计费规则（OpenAI cookbook）：每条消息固定开销 4 + role + content + 可选 name 的 token，
// 总和再加 priming 2。请求体解析失败时回退到字节/4。
func CountChatMessages(body []byte, model string) int {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Name    string `json:"name"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return len(body) / 4
	}
	total := 0
	for _, m := range req.Messages {
		total += 4
		total += CountText(m.Role, model)
		total += CountText(m.Content, model)
		if m.Name != "" {
			total += CountText(m.Name, model) + 1
		}
	}
	total += 2 // priming
	return total
}

// CountEmbeddingsInput 从 embeddings 请求体计算 input 文本的 token 总数。
// 支持 string 或 []string 形式的 input；其他形态回退字节/4。
func CountEmbeddingsInput(body []byte, model string) int {
	var req struct {
		Input any `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return len(body) / 4
	}
	switch v := req.Input.(type) {
	case string:
		return CountText(v, model)
	case []any:
		total := 0
		for _, s := range v {
			if str, ok := s.(string); ok {
				total += CountText(str, model)
			}
		}
		return total
	}
	return len(body) / 4
}
